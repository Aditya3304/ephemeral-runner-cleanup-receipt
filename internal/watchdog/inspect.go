package watchdog

import "time"

// Inspection carries only public result references and bounded retry metadata.
type Inspection struct {
	ReceiptID       string     `json:"receipt_id"`
	Published       bool       `json:"published"`
	FinalizerStatus string     `json:"finalizer_status"`
	FinalizerStage  string     `json:"finalizer_stage"`
	FinalizerTries  int        `json:"finalizer_tries"`
	FinalizerDue    *time.Time `json:"finalizer_due"`
	DeliveryStatus  string     `json:"delivery_status"`
	DeliveryDue     *time.Time `json:"delivery_due"`
}
