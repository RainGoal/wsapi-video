package commerce

import (
	"time"
	"wsapi-video/internal/media"
)

type Capabilities struct {
	AspectRatios                []string `json:"aspect_ratios"`
	Durations                   []int    `json:"durations"`
	Resolutions                 []string `json:"resolutions"`
	Sizes                       []string `json:"sizes"`
	Qualities                   []string `json:"qualities"`
	MaxImages                   int      `json:"max_images"`
	MaxVideos                   int      `json:"max_videos"`
	MaxAudios                   int      `json:"max_audios"`
	SupportsAudio               bool     `json:"supports_audio"`
	SupportsFrames              bool     `json:"supports_frames"`
	SupportsCancel              bool     `json:"supports_cancel"`
	MaxReferenceDurationSeconds int      `json:"max_reference_duration_seconds,omitempty"`
	MaxReferenceBytes           int64    `json:"max_reference_bytes,omitempty"`
	MaxReferenceWidth           int      `json:"max_reference_width,omitempty"`
	MaxReferenceHeight          int      `json:"max_reference_height,omitempty"`
}
type Product struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Kind         string       `json:"kind"`
	Model        string       `json:"model"`
	Credits      int64        `json:"credits"`
	Version      int          `json:"version"`
	Capabilities Capabilities `json:"capabilities"`
	Active       bool         `json:"active"`
	Verified     bool         `json:"verified"`
	Demo         bool         `json:"demo"`
}
type QuoteInput struct {
	ProjectID         string   `json:"project_id"`
	ShotID            string   `json:"shot_id,omitempty"`
	ProductID         string   `json:"product_id"`
	Prompt            string   `json:"prompt"`
	Duration          int      `json:"duration,omitempty"`
	Resolution        string   `json:"resolution,omitempty"`
	AspectRatio       string   `json:"aspect_ratio,omitempty"`
	Size              string   `json:"size,omitempty"`
	Quality           string   `json:"quality,omitempty"`
	Audio             *bool    `json:"audio,omitempty"`
	ReferenceAssetIDs []string `json:"reference_asset_ids,omitempty"`
	StartFrameAssetID string   `json:"start_frame_asset_id,omitempty"`
	EndFrameAssetID   string   `json:"end_frame_asset_id,omitempty"`
}
type Quote struct {
	ID        string    `json:"id"`
	ProductID string    `json:"product_id"`
	Credits   int64     `json:"credits"`
	InputHash string    `json:"input_hash"`
	ExpiresAt time.Time `json:"expires_at"`
	Demo      bool      `json:"demo"`
}
type Task struct {
	ID                string         `json:"id"`
	ProjectID         string         `json:"project_id"`
	ProductID         string         `json:"product_id"`
	Kind              string         `json:"kind"`
	Model             string         `json:"model"`
	Prompt            string         `json:"prompt"`
	Credits           int64          `json:"credits"`
	ExecutionStatus   string         `json:"execution_status"`
	DeliveryStatus    string         `json:"delivery_status"`
	SettlementStatus  string         `json:"settlement_status"`
	ProcurementStatus string         `json:"procurement_status"`
	AssetIDs          []string       `json:"asset_ids"`
	ErrorCode         string         `json:"error_code"`
	ErrorMessage      string         `json:"error_message"`
	Demo              bool           `json:"demo"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	UserID            string         `json:"-"`
	CredentialRef     string         `json:"-"`
	UpstreamID        string         `json:"-"`
	LeaseToken        string         `json:"-"`
	Attempts          int            `json:"-"`
	Input             QuoteInput     `json:"input"`
	Product           Product        `json:"product_snapshot"`
	Outputs           []media.Output `json:"-"`
}
type Wallet struct {
	Available int64 `json:"available"`
	Frozen    int64 `json:"frozen"`
	Demo      bool  `json:"demo"`
}
type Ledger struct {
	ID             string    `json:"id"`
	EventType      string    `json:"event_type"`
	AvailableDelta int64     `json:"available_delta"`
	FrozenDelta    int64     `json:"frozen_delta"`
	AvailableAfter int64     `json:"available_after"`
	FrozenAfter    int64     `json:"frozen_after"`
	TaskID         string    `json:"task_id,omitempty"`
	OrderID        string    `json:"order_id,omitempty"`
	Reason         string    `json:"reason"`
	CreatedAt      time.Time `json:"created_at"`
}
type Tier struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AmountFen int64  `json:"amount_fen"`
	Credits   int64  `json:"credits"`
	Active    bool   `json:"active"`
	Version   int    `json:"version"`
}
type Order struct {
	ID             string     `json:"id"`
	OutTradeNo     string     `json:"out_trade_no"`
	AmountFen      int64      `json:"amount_fen"`
	Credits        int64      `json:"credits"`
	Method         string     `json:"method"`
	PaymentStatus  string     `json:"payment_status"`
	CreditStatus   string     `json:"credit_status"`
	CheckoutURL    string     `json:"checkout_url"`
	CreatedAt      time.Time  `json:"created_at"`
	ExpiresAt      time.Time  `json:"expires_at"`
	PaidAt         *time.Time `json:"paid_at"`
	CreditedAt     *time.Time `json:"credited_at"`
	UserID         string     `json:"-"`
	CredentialRef  string     `json:"-"`
	MerchantPID    string     `json:"-"`
	ProviderOrigin string     `json:"-"`
}
