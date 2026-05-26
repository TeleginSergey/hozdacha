package moysklad

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TeleginSergey/hozdacha/internal/resilience"
	"go.uber.org/zap"
)

type Client struct {
	baseURL     string
	token       string
	client      *http.Client
	logger      *zap.Logger
	rateLimiter *rateLimiter
	maxRetries  int
	breaker     *resilience.CircuitBreaker
	outboundSem chan struct{}
	// organizationID — UUID организации в МойСклад, от имени которой создаются заказы.
	// agentID — UUID контрагента-покупателя по умолчанию (например, "Розничный покупатель").
	// Оба обязательны для CustomerOrder API; если не заданы, создание заказа в МойСклад вернёт ошибку.
	organizationID string
	agentID        string
}

// ClientOption настраивает клиент МойСклад.
type ClientOption func(*Client)

// WithOutboundConcurrency ограничивает одновременные исходящие HTTP-запросы.
func WithOutboundConcurrency(n int) ClientOption {
	return func(c *Client) {
		if n < 1 {
			n = 8
		}
		c.outboundSem = make(chan struct{}, n)
	}
}

// WithCircuitBreaker включает circuit breaker на уровне логического запроса (после исчерпания ретраев).
func WithCircuitBreaker(b *resilience.CircuitBreaker) ClientOption {
	return func(c *Client) {
		c.breaker = b
	}
}

// WithOrderDefaults задаёт UUID организации и контрагента, которые подставляются
// при создании CustomerOrder. Без них API МойСклад заказ не примет.
func WithOrderDefaults(organizationID, agentID string) ClientOption {
	return func(c *Client) {
		c.organizationID = organizationID
		c.agentID = agentID
	}
}

// rateLimiter контролирует частоту запросов к API МойСклад
type rateLimiter struct {
	lastRequest time.Time
	mu          sync.Mutex
	minInterval time.Duration // Минимальный интервал между запросами
}

func newRateLimiter(requestsPerSecond float64) *rateLimiter {
	return &rateLimiter{
		minInterval: time.Duration(1000.0/requestsPerSecond) * time.Millisecond,
	}
}

func (rl *rateLimiter) wait() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(rl.lastRequest)

	if elapsed < rl.minInterval {
		time.Sleep(rl.minInterval - elapsed)
	}

	rl.lastRequest = time.Now()
}

func NewClient(baseURL, token string, logger *zap.Logger, requestsPerSecond float64, maxRetries int, opts ...ClientOption) *Client {
	c := &Client{
		baseURL: baseURL,
		token:   token,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
		logger:      logger,
		rateLimiter: newRateLimiter(requestsPerSecond),
		maxRetries:  maxRetries,
	}
	for _, o := range opts {
		o(c)
	}
	if c.outboundSem == nil {
		c.outboundSem = make(chan struct{}, 8)
	}
	return c
}

func (c *Client) acquireOutbound() {
	c.outboundSem <- struct{}{}
}

func (c *Client) releaseOutbound() {
	<-c.outboundSem
}

type MoyskladProductFolder struct {
	Meta struct {
		Href string `json:"href"`
	} `json:"meta"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

type MoyskladProduct struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Description   *string                `json:"description"`
	SalePrices    []MoyskladSalePrice    `json:"salePrices,omitempty"`
	Stock         *MoyskladStock         `json:"stock,omitempty"` // Остатки как вложенный объект (при expand=stock)
	Images        *MoyskladImages        `json:"images,omitempty"`
	ProductFolder *MoyskladProductFolder `json:"productFolder,omitempty"` // Группа/категория товара
	Updated       string                 `json:"updated,omitempty"`       // Дата обновления
	Created       string                 `json:"created,omitempty"`       // Дата создания
	Href          string                 `json:"href,omitempty"`
}

type MoyskladStock struct {
	Stock        float64 `json:"stock"`     // Общий остаток
	Reserve      float64 `json:"reserve"`   // В резерве
	InTransit    float64 `json:"inTransit"` // В пути
	Available    float64 `json:"available"` // Доступно
	StockByStore []struct {
		Store struct {
			Meta struct {
				Href string `json:"href"`
			} `json:"meta"`
		} `json:"store"`
		Stock float64 `json:"stock"`
	} `json:"stockByStore,omitempty"`
}

type MoyskladSalePrice struct {
	Value     float64 `json:"value"` // Цена в копейках
	PriceType struct {
		Name string `json:"name"`
	} `json:"priceType,omitempty"`
}

type MoyskladImages struct {
	Meta struct {
		Href string `json:"href"`
	} `json:"meta"`
	Rows []MoyskladImage `json:"rows,omitempty"`
}

type MoyskladImage struct {
	Meta struct {
		Href string `json:"href"`
	} `json:"meta"`
	Title string `json:"title"`
}

// MoyskladImageDetail — полная информация об изображении (ответ GET /entity/product/{id}/images/{imageId}).
type MoyskladImageDetail struct {
	Filename  string `json:"filename"`
	Content   string `json:"content,omitempty"` // base64
	Miniature struct {
		Href string `json:"href"`
	} `json:"miniature"`
	Tiny struct {
		Href string `json:"href"`
	} `json:"tiny"`
}

type MoyskladMeta struct {
	Href string `json:"href"`
	Type string `json:"type"`
}

type MoyskladOrderRequest struct {
	Name         string             `json:"name"`
	Description  string             `json:"description,omitempty"`
	Positions    []MoyskladPosition `json:"positions"`
	Organization *MoyskladEntity    `json:"organization"`
	Agent        *MoyskladEntity    `json:"agent"`
}

type MoyskladOrder struct {
	ID           string          `json:"id,omitempty"`
	Name         string          `json:"name,omitempty"`
	Description  string          `json:"description,omitempty"`
	Positions    json.RawMessage `json:"positions,omitempty"`
	State        *MoyskladState  `json:"state,omitempty"`
	Organization *MoyskladEntity `json:"organization,omitempty"`
	Agent        *MoyskladEntity `json:"agent,omitempty"`
}

type MoyskladEntity struct {
	Meta *OrderMeta `json:"meta"`
}

type MoyskladPosition struct {
	Quantity   int             `json:"quantity"`
	Assortment *MoyskladEntity `json:"assortment"`
	Price      float64         `json:"price"`
	// Reserve — количество товара, которое должно быть зарезервировано на складе.
	// Обычно равно Quantity. Именно это поле делает заказ "бронью" в МойСклад
	// (товар уйдёт в "в резерве" в отчётах остатков).
	Reserve int `json:"reserve,omitempty"`
}

type MoyskladState struct {
	Meta struct {
		Href string `json:"href"`
		Type string `json:"type"`
	} `json:"meta"`
}

type MoyskladResponse struct {
	Rows []MoyskladProduct `json:"rows"`
	Meta struct {
		Size         int    `json:"size"`
		Limit        int    `json:"limit"`
		Offset       int    `json:"offset"`
		NextHref     string `json:"nextHref,omitempty"`
		PreviousHref string `json:"previousHref,omitempty"`
	} `json:"meta"`
}

type MoyskladOrderResponse struct {
	ID string `json:"id"`
}

func parseRetryAfterSeconds(h string, attempt int) time.Duration {
	if h == "" {
		return 0
	}
	if sec, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && sec > 0 {
		return time.Duration(sec) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}
	return resilience.MoyskladHTTPRetrySleepDuration(attempt)
}

func (c *Client) doRequestWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	maxRetries := c.maxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if c.breaker != nil && !c.breaker.Allow() {
			return nil, resilience.ErrCircuitOpen
		}

		c.rateLimiter.wait()
		c.acquireOutbound()

		r := req.Clone(ctx)
		if r == nil {
			r = req
		}

		resp, err := c.client.Do(r)
		if err != nil {
			c.releaseOutbound()
			lastErr = err
			c.logger.Warn("Moysklad request failed", zap.Int("attempt", attempt+1), zap.Error(err))
			if attempt == maxRetries {
				if c.breaker != nil {
					c.breaker.OnFailure()
				}
				return nil, fmt.Errorf("failed after %d attempts: %w", maxRetries+1, err)
			}
			_ = resilience.MoyskladHTTPRetrySleep(ctx, attempt)
			continue
		}

		switch resp.StatusCode {
		case http.StatusTooManyRequests:
			ra := parseRetryAfterSeconds(resp.Header.Get("Retry-After"), attempt)
			resp.Body.Close()
			c.releaseOutbound()
			c.logger.Warn("Moysklad 429", zap.Int("attempt", attempt+1), zap.Duration("delay", ra))
			if attempt == maxRetries {
				if c.breaker != nil {
					c.breaker.OnFailure()
				}
				return nil, fmt.Errorf("rate limit exceeded after %d attempts", maxRetries+1)
			}
			if ra > 0 {
				_ = resilience.SleepCtx(ctx, ra)
			} else {
				_ = resilience.MoyskladHTTPRetrySleep(ctx, attempt)
			}
			continue

		case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
			resp.Body.Close()
			c.releaseOutbound()
			c.logger.Warn("Moysklad temporary HTTP error", zap.Int("status", resp.StatusCode), zap.Int("attempt", attempt+1))
			if attempt == maxRetries {
				if c.breaker != nil {
					c.breaker.OnFailure()
				}
				return nil, fmt.Errorf("moysklad server error %d after %d attempts", resp.StatusCode, maxRetries+1)
			}
			_ = resilience.MoyskladHTTPRetrySleep(ctx, attempt)
			continue

		default:
			c.releaseOutbound()
			if c.breaker != nil {
				switch {
				case resp.StatusCode >= 200 && resp.StatusCode < 300:
					c.breaker.OnSuccess()
				case resp.StatusCode >= 500:
					c.breaker.OnFailure()
				default:
					c.breaker.OnSuccess()
				}
			}
			return resp, nil
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("unexpected retry loop exit: %w", lastErr)
	}
	return nil, fmt.Errorf("unexpected retry loop exit")
}

func (c *Client) GetProducts(ctx context.Context) ([]MoyskladProduct, error) {
	allProducts := make([]MoyskladProduct, 0)
	limit := 100 // Максимальный лимит для МойСклад API
	offset := 0

	for {
		url := fmt.Sprintf("%s/entity/product", c.baseURL)

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/json;charset=utf-8")
		req.Header.Set("Content-Type", "application/json")

		// Добавляем параметры для пагинации
		q := req.URL.Query()
		q.Set("limit", fmt.Sprintf("%d", limit))
		q.Set("offset", fmt.Sprintf("%d", offset))
		req.URL.RawQuery = q.Encode()

		resp, err := c.doRequestWithRetry(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("failed to execute request: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("moysklad API error: %s, body: %s", resp.Status, string(body))
		}

		var response MoyskladResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		allProducts = append(allProducts, response.Rows...)

		// Если получили меньше товаров, чем лимит, значит это последняя страница
		if len(response.Rows) < limit {
			break
		}

		// Если нет nextHref, значит больше страниц нет
		if response.Meta.NextHref == "" {
			break
		}

		offset += limit

		// Защита от бесконечного цикла
		if offset > 100000 {
			c.logger.Warn("Reached maximum offset limit, stopping pagination")
			break
		}
	}

	c.logger.Info("Fetched all products from Moysklad", zap.Int("total", len(allProducts)))
	return allProducts, nil
}

// GetProductsByID получает товары по ID (для webhook'ов)
func (c *Client) GetProductsByID(ctx context.Context, moyskladID string) ([]MoyskladProduct, error) {
	url := fmt.Sprintf("%s/entity/product/%s", c.baseURL, moyskladID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json;charset=utf-8")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("moysklad API error: %s, body: %s", resp.Status, string(body))
	}

	var product MoyskladProduct
	if err := json.NewDecoder(resp.Body).Decode(&product); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return []MoyskladProduct{product}, nil
}

// GetProductsDelta получает товары, обновленные после указанного времени
func (c *Client) GetProductsDelta(ctx context.Context, since time.Time) ([]MoyskladProduct, error) {
	allProducts := make([]MoyskladProduct, 0)
	limit := 100
	offset := 0

	for {
		url := fmt.Sprintf("%s/entity/product", c.baseURL)

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/json;charset=utf-8")
		req.Header.Set("Content-Type", "application/json")

		// Добавляем фильтр по updated и параметры для пагинации
		q := req.URL.Query()
		// МойСклад требует формат: 2024-01-01 00:00:00
		// Используем UTC без миллисекунд и таймзоны
		updatedFilter := since.UTC().Format("2006-01-02 15:04:05")
		q.Set("filter", fmt.Sprintf("updated>%s", updatedFilter))
		q.Set("limit", fmt.Sprintf("%d", limit))
		q.Set("offset", fmt.Sprintf("%d", offset))
		req.URL.RawQuery = q.Encode()

		resp, err := c.doRequestWithRetry(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("failed to execute request: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode == http.StatusGone {
				return nil, fmt.Errorf("%w: %s", ErrResyncRequired, string(body))
			}
			return nil, fmt.Errorf("moysklad API error: %s, body: %s", resp.Status, string(body))
		}

		var response MoyskladResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		allProducts = append(allProducts, response.Rows...)

		// Если получили меньше товаров, чем лимит, значит это последняя страница
		if len(response.Rows) < limit {
			break
		}

		// Если нет nextHref, значит больше страниц нет
		if response.Meta.NextHref == "" {
			break
		}

		offset += limit

		// Защита от бесконечного цикла
		if offset > 100000 {
			c.logger.Warn("Reached maximum offset limit, stopping pagination")
			break
		}
	}

	c.logger.Info("Fetched delta products from Moysklad", zap.Int("total", len(allProducts)))
	return allProducts, nil
}

// GetStockReportFromURL получает отчет по остаткам из указанного URL (для webhookstock)
func (c *Client) GetStockReportFromURL(ctx context.Context, reportURL string) (map[string]float64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", reportURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	var stockReport struct {
		Rows []struct {
			Stock     float64 `json:"stock"`
			ProductID struct {
				Meta struct {
					Href string `json:"href"`
				} `json:"meta"`
			} `json:"assortment"`
		} `json:"rows"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&stockReport); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	stockMap := make(map[string]float64)
	for _, row := range stockReport.Rows {
		// Извлекаем ID товара из href
		parts := strings.Split(row.ProductID.Meta.Href, "/")
		if len(parts) > 0 {
			productID := parts[len(parts)-1]
			stockMap[productID] = row.Stock
		}
	}

	return stockMap, nil
}

// GetStockReport получает карту остатков (stock) по товарам/вариантам.
// Используем /report/stock/all, так как entity/product не возвращает остатки.
func (c *Client) GetStockReport(ctx context.Context) (map[string]float64, error) {
	limit := 1000
	offset := 0
	stockMap := make(map[string]float64)

	for {
		endpoint := fmt.Sprintf("%s/report/stock/all", c.baseURL)

		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/json;charset=utf-8")
		req.Header.Set("Content-Type", "application/json")

		q := req.URL.Query()
		q.Set("limit", fmt.Sprintf("%d", limit))
		q.Set("offset", fmt.Sprintf("%d", offset))
		req.URL.RawQuery = q.Encode()

		resp, err := c.doRequestWithRetry(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("failed to execute request: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("moysklad API error: %s, body: %s", resp.Status, string(body))
		}

		var response struct {
			Rows []struct {
				Meta struct {
					Href string `json:"href"`
					Type string `json:"type"`
				} `json:"meta"`
				Stock float64 `json:"stock"`
			} `json:"rows"`
			Meta struct {
				Size     int    `json:"size"`
				Limit    int    `json:"limit"`
				Offset   int    `json:"offset"`
				NextHref string `json:"nextHref,omitempty"`
			} `json:"meta"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		for _, row := range response.Rows {
			id := c.extractIDFromHref(row.Meta.Href)
			if id != "" {
				stockMap[id] = row.Stock
			}
		}

		if len(response.Rows) < limit || response.Meta.NextHref == "" {
			break
		}
		offset += limit
		if offset > 500000 {
			c.logger.Warn("Reached maximum offset limit in stock report, stopping pagination")
			break
		}
	}

	return stockMap, nil
}

func (c *Client) extractIDFromHref(href string) string {
	// href часто содержит query (?expand=...), его нужно убрать.
	if href == "" {
		return ""
	}
	if u, err := url.Parse(href); err == nil && u.Path != "" {
		// Последний сегмент path
		p := strings.TrimSuffix(u.Path, "/")
		if idx := strings.LastIndex(p, "/"); idx >= 0 && idx < len(p)-1 {
			return p[idx+1:]
		}
	}

	// fallback: просто обрезаем query вручную
	cut := href
	if i := strings.IndexByte(cut, '?'); i >= 0 {
		cut = cut[:i]
	}
	cut = strings.TrimSuffix(cut, "/")
	if idx := strings.LastIndex(cut, "/"); idx >= 0 && idx < len(cut)-1 {
		return cut[idx+1:]
	}
	return ""
}

// HasOrderDefaults сообщает, заданы ли organization/agent (без них CustomerOrder в МойСклад не создаётся).
func (c *Client) HasOrderDefaults() bool {
	return c.organizationID != "" && c.agentID != ""
}

func (c *Client) GetOrganizations(ctx context.Context) (*MoyskladResponse, error) {
	url := fmt.Sprintf("%s/entity/organization", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json;charset=utf-8")

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("moysklad API error: %s, body: %s", resp.Status, string(body))
	}

	var response MoyskladResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response, nil
}

func (c *Client) GetCounterparties(ctx context.Context) (*MoyskladResponse, error) {
	url := fmt.Sprintf("%s/entity/counterparty", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json;charset=utf-8")

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("moysklad API error: %s, body: %s", resp.Status, string(body))
	}

	var response MoyskladResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &response, nil
}

func (c *Client) CreateCustomerOrder(ctx context.Context, order *MoyskladOrderRequest) (*MoyskladOrder, error) {
	orderJSON, err := json.Marshal(order)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal order: %w", err)
	}

	url := fmt.Sprintf("%s/entity/customerorder", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(orderJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json;charset=utf-8")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("moysklad API error: %s, body: %s", resp.Status, string(body))
	}

	var response MoyskladOrder
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	c.logger.Info("Order created in Moysklad", zap.String("moysklad_id", response.ID))
	return &response, nil
}

// DeleteCustomerOrder удаляет CustomerOrder из МойСклад.
// Используется когда у клиента истёк TTL брони (он не пришёл в магазин).
// Согласно https://dev.moysklad.ru/doc/api/remap/1.2/#mojsklad-json-api-obschie-swedeniq-udalenie-ob-ekta
// удалённый ресурс возвращает 200 OK c пустым телом.
func (c *Client) DeleteCustomerOrder(ctx context.Context, moyskladID string) error {
	if moyskladID == "" {
		return fmt.Errorf("empty moysklad id")
	}
	url := fmt.Sprintf("%s/entity/customerorder/%s", c.baseURL, moyskladID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json;charset=utf-8")

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// 200 — удалён, 404 — уже нет (идемпотентно), всё остальное — ошибка.
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		c.logger.Info("Moysklad customer order deleted",
			zap.String("moysklad_id", moyskladID),
			zap.Int("status", resp.StatusCode))
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("moysklad delete order failed: %s, body: %s", resp.Status, string(body))
}

func (c *Client) GetBaseURL() string {
	return c.baseURL
}

// GetProductFirstDownloadURL возвращает meta.downloadHref (ссылку на оригинал изображения)
// первого фото товара из /entity/product/{id}/images.
// Возвращает пустую строку если у товара нет изображений.
func (c *Client) GetProductFirstDownloadURL(ctx context.Context, moyskladProductID string) (string, error) {
	endpoint := fmt.Sprintf("%s/entity/product/%s/images?limit=1", c.baseURL, moyskladProductID)
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json;charset=utf-8")

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to get product images: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("moysklad images API error: %s, body: %s", resp.Status, string(body))
	}

	var result struct {
		Rows []struct {
			Meta struct {
				DownloadHref string `json:"downloadHref"`
				Href         string `json:"href"`
			} `json:"meta"`
		} `json:"rows"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode images response: %w", err)
	}
	if len(result.Rows) == 0 {
		return "", nil
	}
	// Предпочитаем downloadHref (оригинал), fallback на href если downloadHref пуст.
	if result.Rows[0].Meta.DownloadHref != "" {
		return result.Rows[0].Meta.DownloadHref, nil
	}
	return result.Rows[0].Meta.Href, nil
}

// DownloadRawFile скачивает файл по URL с авторизацией.
// Возвращает байты и Content-Type.
func (c *Client) DownloadRawFile(ctx context.Context, downloadURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("download error %s: %s", resp.Status, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read file data: %w", err)
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}
	return data, contentType, nil
}

// DownloadImage скачивает изображение товара из МойСклад по href изображения.
// Возвращает байты изображения и его формат (из Content-Type).
func (c *Client) DownloadImage(ctx context.Context, imageHref string) ([]byte, string, error) {
	// Сначала получаем метаданные изображения (miniature URL для скачивания).
	metaReq, err := http.NewRequestWithContext(ctx, "GET", imageHref, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}
	metaReq.Header.Set("Authorization", "Bearer "+c.token)
	metaReq.Header.Set("Accept", "application/json;charset=utf-8")

	metaResp, err := c.doRequestWithRetry(ctx, metaReq)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get image metadata: %w", err)
	}
	defer metaResp.Body.Close()

	if metaResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(metaResp.Body)
		return nil, "", fmt.Errorf("moysklad image metadata error: %s, body: %s", metaResp.Status, string(body))
	}

	var detail MoyskladImageDetail
	if err := json.NewDecoder(metaResp.Body).Decode(&detail); err != nil {
		return nil, "", fmt.Errorf("failed to decode image metadata: %w", err)
	}

	// Скачиваем миниатюру (меньше размер — экономим RAM и диск).
	downloadURL := detail.Miniature.Href
	if downloadURL == "" {
		downloadURL = detail.Tiny.Href
	}
	if downloadURL == "" {
		return nil, "", fmt.Errorf("no download URL in image metadata")
	}

	imgReq, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create download request: %w", err)
	}
	imgReq.Header.Set("Authorization", "Bearer "+c.token)

	imgResp, err := c.doRequestWithRetry(ctx, imgReq)
	if err != nil {
		return nil, "", fmt.Errorf("failed to download image: %w", err)
	}
	defer imgResp.Body.Close()

	if imgResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(imgResp.Body)
		return nil, "", fmt.Errorf("moysklad image download error: %s, body: %s", imgResp.Status, string(body))
	}

	data, err := io.ReadAll(imgResp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read image data: %w", err)
	}

	contentType := imgResp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}

	return data, contentType, nil
}

// ProductFolderItem — группа товаров (категория) из /entity/productfolder.
type ProductFolderItem struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	PathName string `json:"pathName"` // полный путь, например "Группа/Подгруппа"
}

// GetProductFolders возвращает все группы товаров из МойСклад.
// Использует отдельный endpoint /entity/productfolder — надёжнее expand.
func (c *Client) GetProductFolders(ctx context.Context) ([]ProductFolderItem, error) {
	type folderResponse struct {
		Rows []ProductFolderItem `json:"rows"`
		Meta struct {
			Size   int `json:"size"`
			Limit  int `json:"limit"`
			Offset int `json:"offset"`
		} `json:"meta"`
	}

	var all []ProductFolderItem
	limit := 100
	offset := 0

	for {
		endpoint := fmt.Sprintf("%s/entity/productfolder", c.baseURL)
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create productfolder request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/json;charset=utf-8")

		q := req.URL.Query()
		q.Set("limit", fmt.Sprintf("%d", limit))
		q.Set("offset", fmt.Sprintf("%d", offset))
		req.URL.RawQuery = q.Encode()

		resp, err := c.doRequestWithRetry(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch product folders: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("moysklad productfolder API error: %s, body: %s", resp.Status, string(body))
		}

		var result folderResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("failed to decode productfolder response: %w", err)
		}

		all = append(all, result.Rows...)
		if len(result.Rows) < limit {
			break
		}
		offset += limit
		if offset > 10000 {
			break
		}
	}

	c.logger.Info("Fetched product folders from Moysklad", zap.Int("total", len(all)))
	return all, nil
}
