package engine

import (
	"context"
	"errors"
	"fmt"
)

// Engine 是 Noema 业务层看到的唯一执行入口。
// Lucky Agent SDK 的类型不得越过 adapters/lucky。
type Engine interface {
	NewSession(ctx context.Context) (string, error)
	StartTurn(ctx context.Context, request RunRequest) (<-chan RunEvent, error)
	ResolveApproval(ctx context.Context, decision ApprovalDecision) error
	CancelTurn(ctx context.Context, runID string) error
	Capabilities() Capabilities
	Close() error
}

// RunRequest 是一轮用户输入。HostContext 在当前 SDK pin 上必须被拒绝。
type RunRequest struct {
	OwnerID         string
	CharacterID     string
	SessionID       string
	Message         string
	ClientRequestID string
	HostContext     *HostContext
}

// HostContext 是拟新增的逐轮宿主上下文。当前 Lucky Agent SDK 没有对应字段。
type HostContext struct{}

// ApprovalDecision 绑定一次具体工具动作。当前 SDK 没有回传 API。
type ApprovalDecision struct {
	RunID     string
	RequestID string
	Approved  bool
}

// RunEvent 是适配层规范化后的流式事件。没有 ui_delta 或角色表情。
type RunEvent struct {
	Type            EventType
	RunID           string
	SessionID       string
	ClientRequestID string
	Content         string
	Err             error
}

type EventType string

const (
	EventContent   EventType = "content"
	EventDone      EventType = "done"
	EventError     EventType = "error"
	EventCancelled EventType = "cancelled"
	EventApproval  EventType = "approval_required"
	EventUnknown   EventType = "unknown"
)

// Capabilities 报告实际可用功能。未实现的入口必须返回明确错误，不能做成空成功。
type Capabilities struct {
	TextStream         bool `json:"text_stream"`
	CancelTurn         bool `json:"cancel_turn"`
	IsolatedHomeDir    bool `json:"isolated_home_dir"`
	HostContext        bool `json:"host_context"`
	UIDelta            bool `json:"ui_delta"`
	ApprovalResolution bool `json:"approval_resolution"`
	ContextualTools    bool `json:"contextual_tools"`
	ControlledMemory   bool `json:"controlled_memory"`
}

var (
	ErrUnsupported    = errors.New("unsupported")
	ErrInvalidRequest = errors.New("invalid request")
	ErrNotFound       = errors.New("not found")
)

func Unsupported(feature string) error {
	return fmt.Errorf("%w: %s", ErrUnsupported, feature)
}
