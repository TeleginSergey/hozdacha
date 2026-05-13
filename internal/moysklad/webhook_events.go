package moysklad

// WebhookEntityEnvelope — тело вебхука сущностей МойСклад.
type WebhookEntityEnvelope struct {
	Events []WebhookEntityEvent `json:"events"`
}

// WebhookEntityEvent — одно событие внутри envelope.
type WebhookEntityEvent struct {
	Meta struct {
		Type string `json:"type"`
		Href string `json:"href"`
	} `json:"meta"`
	Action    string `json:"action"`
	AccountID string `json:"accountId"`
}

// WebhookStockPayload — webhookstock (остатки).
type WebhookStockPayload struct {
	AccountID  string `json:"accountId"`
	StockType  string `json:"stockType"`
	ReportType string `json:"reportType"`
	ReportURL  string `json:"reportUrl"`
}
