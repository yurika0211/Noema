package estuary

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/yurika0211/estuary/memoryindex"
)

// Hit 是某一主体索引里的一条命中。不含其他主体的正文。
type Hit struct {
	SubjectID string
	EventID   string
	Content   string
	Score     float64
}

// Index 按 subject_id 各开一个 SQLite 文件。
// 路径是 NOEMA_STATE_DIR/estuary/index/<subject>.sqlite，不在 Lucky HomeDir。
// 本切片用本地 hash embedder，不调用 RAG_URL，也不把命中送进模型。
type Index struct {
	mu    sync.Mutex
	dir   string
	emb   memoryindex.Embedder
	files map[string]*memoryindex.Index
}

// OpenIndex 打开状态目录下的主体索引。embedder 为空时用本地 hash，不访问网络。
func OpenIndex(stateDir string, emb memoryindex.Embedder) (*Index, error) {
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
	return &Index{dir: abs, emb: emb, files: make(map[string]*memoryindex.Index)}, nil
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
	title := rec.Kind
	if title == "" {
		title = rec.Namespace
	}
	return store.IndexText(ctx, rec.EventID, title, text)
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
	rows, err := store.Search(ctx, query, 5)
	if err != nil {
		return nil, err
	}
	out := make([]Hit, 0, len(rows))
	for _, row := range rows {
		out = append(out, Hit{
			SubjectID: subjectID,
			EventID:   row.Source,
			Content:   row.Content,
			Score:     row.Score,
		})
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
		if err := store.Close(); err != nil && first == nil {
			first = err
		}
		delete(idx.files, subject)
	}
	return first
}

func (idx *Index) subject(subjectID string) (*memoryindex.Index, error) {
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
	store, err := memoryindex.Open(path, idx.emb)
	if err != nil {
		return nil, fmt.Errorf("estuary: open subject index %s: %w", subjectID, err)
	}
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

func (h HashEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i, text := range texts {
		vec, err := h.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		out[i] = vec
	}
	return out, nil
}
