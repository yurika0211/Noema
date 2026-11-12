package estuary

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

// Hit 是某一主体索引里的一条命中。不含其他主体的正文。
type Hit struct {
	SubjectID string
	EventID   string
	Content   string
	Score     float64
}

// Embedder 产生查询与正文的向量。测试传入确定性实现；默认是本地 hash。
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	Dimension() int
	Name() string
	Model() string
}

// Index 按 subject_id 各开一个 SQLite 文件。
// 路径是 NOEMA_STATE_DIR/estuary/index/<subject>.sqlite，不在 Lucky HomeDir。
// 本切片用本地 hash embedder，不调用 RAG_URL，也不把命中送进模型。
// 私有 estuary 模块还不能被公开 CI 拉取，所以投影留在 Noema，不复制 RAG 实现。
type Index struct {
	mu    sync.Mutex
	dir   string
	emb   Embedder
	files map[string]*subjectDB
}

type subjectDB struct {
	db *sql.DB
}

// OpenIndex 打开状态目录下的主体索引。embedder 为空时用本地 hash，不访问网络。
func OpenIndex(stateDir string, emb Embedder) (*Index, error) {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return nil, fmt.Errorf("estuary: state dir is required")
	}
	abs, err := filepath.Abs(stateDir)
	if err != nil {
		return nil, fmt.Errorf("estuary: resolve state dir: %w", err)
	}
	dir := filepath.Join(abs, "estuary", "index")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("estuary: create index dir: %w", err)
	}
	if emb == nil {
		emb = HashEmbedder{Dim: 64}
	}
	return &Index{dir: abs, emb: emb, files: make(map[string]*subjectDB)}, nil
}

// Project 把一条已写入账本的记录投进该主体自己的索引。
// fictional=true 只留在 jsonl，不进现实索引。
func (idx *Index) Project(ctx context.Context, rec Record) error {
	if idx == nil {
		return nil
	}
	if fictionalOf(rec) {
		return nil
	}
	text := payloadText(rec)
	if strings.TrimSpace(text) == "" || strings.TrimSpace(rec.EventID) == "" {
		return nil
	}
	store, err := idx.subject(rec.SubjectID)
	if err != nil {
		return err
	}
	vec, err := idx.emb.Embed(ctx, text)
	if err != nil {
		return fmt.Errorf("estuary: embed record: %w", err)
	}
	raw, err := json.Marshal(vec)
	if err != nil {
		return fmt.Errorf("estuary: encode vector: %w", err)
	}
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO texts(event_id, content, vector) VALUES(?, ?, ?)
		ON CONFLICT(event_id) DO UPDATE SET content=excluded.content, vector=excluded.vector
	`, rec.EventID, text, string(raw))
	if err != nil {
		return fmt.Errorf("estuary: project record: %w", err)
	}
	return nil
}

// Search 只查 subjectID 对应的 sqlite。其他主体的 payload 不在这个文件里。
func (idx *Index) Search(ctx context.Context, subjectID, query string) ([]Hit, error) {
	if idx == nil {
		return nil, fmt.Errorf("estuary: nil index")
	}
	subjectID = strings.TrimSpace(subjectID)
	query = strings.TrimSpace(query)
	if subjectID == "" || query == "" {
		return nil, fmt.Errorf("estuary: subject_id and query are required")
	}
	store, err := idx.subject(subjectID)
	if err != nil {
		return nil, err
	}
	qvec, err := idx.emb.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("estuary: embed query: %w", err)
	}
	rows, err := store.db.QueryContext(ctx, `SELECT event_id, content, vector FROM texts`)
	if err != nil {
		return nil, fmt.Errorf("estuary: search subject index: %w", err)
	}
	defer rows.Close()
	out := make([]Hit, 0)
	for rows.Next() {
		var eventID, content, raw string
		if err := rows.Scan(&eventID, &content, &raw); err != nil {
			return nil, err
		}
		var vec []float64
		if err := json.Unmarshal([]byte(raw), &vec); err != nil {
			continue
		}
		score := cosine(qvec, vec)
		lexical := tokenOverlap(query, content)
		if lexical > score {
			score = lexical
		}
		if score <= 0 {
			continue
		}
		out = append(out, Hit{
			SubjectID: subjectID,
			EventID:   eventID,
			Content:   content,
			Score:     score,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortHits(out)
	if len(out) > 5 {
		out = out[:5]
	}
	return out, nil
}

// Close 关闭已打开的主体库。
func (idx *Index) Close() error {
	if idx == nil {
		return nil
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	var first error
	for subject, store := range idx.files {
		if err := store.db.Close(); err != nil && first == nil {
			first = err
		}
		delete(idx.files, subject)
	}
	return first
}

func (idx *Index) subject(subjectID string) (*subjectDB, error) {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return nil, fmt.Errorf("estuary: subject_id is required")
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if store, ok := idx.files[subjectID]; ok {
		return store, nil
	}
	path := filepath.Join(idx.dir, "estuary", "index", subjectFile(subjectID))
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("estuary: open subject index %s: %w", subjectID, err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS texts (
		event_id TEXT PRIMARY KEY,
		content TEXT NOT NULL,
		vector TEXT NOT NULL
	)`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("estuary: init subject index %s: %w", subjectID, err)
	}
	store := &subjectDB{db: db}
	idx.files[subjectID] = store
	return store, nil
}

func payloadText(rec Record) string {
	if rec.Payload == nil {
		return ""
	}
	text, _ := rec.Payload["text"].(string)
	return text
}

func subjectFile(subjectID string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, subjectID)
	if safe == "" || safe != subjectID {
		sum := sha256.Sum256([]byte(subjectID))
		safe = fmt.Sprintf("s-%x", sum[:6])
	}
	return safe + ".sqlite"
}

func cosine(a, b []float64) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := 0; i < n; i++ {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func tokenOverlap(query, content string) float64 {
	parts := strings.Fields(query)
	if len(parts) == 0 {
		return 0
	}
	hit := 0
	for _, part := range parts {
		if strings.Contains(content, part) {
			hit++
		}
	}
	return float64(hit) / float64(len(parts))
}

func sortHits(hits []Hit) {
	for i := 1; i < len(hits); i++ {
		j := i
		for j > 0 && hits[j].Score > hits[j-1].Score {
			hits[j], hits[j-1] = hits[j-1], hits[j]
			j--
		}
	}
}

// HashEmbedder 是确定性本地向量，不访问网络。缺 RAG_URL / key 时用它，Listen 不因此失败。
type HashEmbedder struct {
	Dim int
}

func (h HashEmbedder) Dimension() int {
	if h.Dim <= 0 {
		return 64
	}
	return h.Dim
}

func (h HashEmbedder) Name() string  { return "noema-hash" }
func (h HashEmbedder) Model() string { return "sha256" }

func (h HashEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	dim := h.Dimension()
	vec := make([]float64, dim)
	var norm float64
	for i := 0; i < dim; i++ {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s#%d", text, i)))
		v := float64(int(sum[0])-128) / 128
		vec[i] = v
		norm += v * v
	}
	if norm == 0 {
		vec[0] = 1
		return vec, nil
	}
	norm = math.Sqrt(norm)
	for i := range vec {
		vec[i] /= norm
	}
	return vec, nil
}
