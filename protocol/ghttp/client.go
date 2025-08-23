package ghttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Config holds the configuration for the HTTP client
type Config struct {
	// Base configuration
	Timeout         time.Duration `json:"timeout" yaml:"timeout"`                       // Request timeout
	MaxRetries      int           `json:"max_retries" yaml:"max_retries"`               // Maximum retry attempts
	RetryDelay      time.Duration `json:"retry_delay" yaml:"retry_delay"`               // Delay between retries
	MaxIdleConns    int           `json:"max_idle_conns" yaml:"max_idle_conns"`         // Max idle connections
	MaxConnsPerHost int           `json:"max_conns_per_host" yaml:"max_conns_per_host"` // Max connections per host

	// TLS and security
	InsecureSkipVerify bool `json:"insecure_skip_verify" yaml:"insecure_skip_verify"`

	// Headers
	DefaultHeaders map[string]string `json:"default_headers" yaml:"default_headers"`

	// Logging
	EnableLogging bool `json:"enable_logging" yaml:"enable_logging"`
	LogLevel      int  `json:"log_level" yaml:"log_level"` // 0=ERROR, 1=WARN, 2=INFO, 3=DEBUG
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		Timeout:         30 * time.Second,
		MaxRetries:      3,
		RetryDelay:      1 * time.Second,
		MaxIdleConns:    100,
		MaxConnsPerHost: 10,
		EnableLogging:   true,
		LogLevel:        2, // INFO level
		DefaultHeaders: map[string]string{
			"User-Agent": "ghttp/1.0",
		},
	}
}

// Response wraps the HTTP response with additional metadata
type Response struct {
	*http.Response
	Body       []byte        `json:"-"`
	Duration   time.Duration `json:"duration"`
	RetryCount int           `json:"retry_count"`
}

// Client is the main HTTP client structure
type Client struct {
	client *http.Client
	config *Config
	logger *log.Logger
	mu     sync.RWMutex
}

var (
	instance *Client
	once     sync.Once
)

// NewClient creates a new HTTP client with the given configuration
func NewClient(config *Config) *Client {
	if config == nil {
		config = DefaultConfig()
	}

	transport := &http.Transport{
		MaxIdleConns:        config.MaxIdleConns,
		MaxIdleConnsPerHost: config.MaxConnsPerHost,
		IdleConnTimeout:     90 * time.Second,
	}

	client := &Client{
		client: &http.Client{
			Transport: transport,
			Timeout:   config.Timeout,
		},
		config: config,
	}

	// Setup logger
	if config.EnableLogging {
		client.logger = log.New(log.Writer(), "[GHTTP] ", log.LstdFlags|log.Lshortfile)
	}

	return client
}

// getConfig returns a copy of the current configuration (thread-safe)
func (c *Client) getConfig() *Config {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a copy to prevent external modification
	configCopy := *c.config

	// Deep copy headers
	if c.config.DefaultHeaders != nil {
		configCopy.DefaultHeaders = make(map[string]string)
		for k, v := range c.config.DefaultHeaders {
			configCopy.DefaultHeaders[k] = v
		}
	}

	return &configCopy
}

// log writes a log message if logging is enabled
func (c *Client) log(level int, format string, args ...any) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.logger != nil && c.config.EnableLogging && level <= c.config.LogLevel {
		levelStr := []string{"ERROR", "WARN", "INFO", "DEBUG"}[level]
		c.logger.Printf("[%s] "+format, append([]any{levelStr}, args...)...)
	}
}

// buildRequest creates an HTTP request with default headers and context
func (c *Client) buildRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Add default headers
	if c.config.DefaultHeaders != nil {
		for key, value := range c.config.DefaultHeaders {
			req.Header.Set(key, value)
		}
	}

	return req, nil
}

// doWithRetry executes the HTTP request with retry mechanism
func (c *Client) doWithRetry(req *http.Request) (*Response, error) {
	var lastErr error
	startTime := time.Now()

	config := c.getConfig() // Get thread-safe copy

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		if attempt > 0 {
			c.log(2, "Retrying request to %s, attempt %d/%d", req.URL.String(), attempt, config.MaxRetries)
			time.Sleep(config.RetryDelay)
		}

		// Clone request for retry (body might be consumed)
		reqClone := req.Clone(req.Context())

		c.log(3, "Sending %s request to %s", req.Method, req.URL.String())

		resp, err := c.client.Do(reqClone)
		if err != nil {
			lastErr = fmt.Errorf("request failed on attempt %d: %w", attempt+1, err)
			c.log(1, "Request failed: %v", err)
			continue
		}

		// Read response body
		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read response body on attempt %d: %w", attempt+1, err)
			c.log(1, "Failed to read response body: %v", err)
			continue
		}

		duration := time.Since(startTime)

		response := &Response{
			Response:   resp,
			Body:       bodyBytes,
			Duration:   duration,
			RetryCount: attempt,
		}

		// Check if we should retry based on status code
		if c.shouldRetry(resp.StatusCode) && attempt < config.MaxRetries {
			lastErr = fmt.Errorf("received retryable status code %d on attempt %d", resp.StatusCode, attempt+1)
			c.log(2, "Received retryable status code %d", resp.StatusCode)
			continue
		}

		c.log(2, "Request completed successfully in %v with status %d", duration, resp.StatusCode)
		return response, nil
	}

	return nil, fmt.Errorf("max retries (%d) exceeded: %w", config.MaxRetries, lastErr)
}

// shouldRetry determines if a request should be retried based on status code
func (c *Client) shouldRetry(statusCode int) bool {
	// Retry on server errors (5xx) and some client errors
	return statusCode >= 500 || statusCode == 408 || statusCode == 429
}

// Get performs a GET request
func (c *Client) Get(ctx context.Context, url string, headers ...map[string]string) (*Response, error) {
	req, err := c.buildRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build GET request: %w", err)
	}

	// Add custom headers
	for _, headerMap := range headers {
		for key, value := range headerMap {
			req.Header.Set(key, value)
		}
	}

	return c.doWithRetry(req)
}

// Post performs a POST request with JSON body
func (c *Client) Post(ctx context.Context, url string, body any, headers ...map[string]string) (*Response, error) {
	return c.doRequest(ctx, http.MethodPost, url, body, headers...)
}

// Put performs a PUT request with JSON body
func (c *Client) Put(ctx context.Context, url string, body any, headers ...map[string]string) (*Response, error) {
	return c.doRequest(ctx, http.MethodPut, url, body, headers...)
}

// Delete performs a DELETE request
func (c *Client) Delete(ctx context.Context, url string, headers ...map[string]string) (*Response, error) {
	req, err := c.buildRequest(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build DELETE request: %w", err)
	}

	// Add custom headers
	for _, headerMap := range headers {
		for key, value := range headerMap {
			req.Header.Set(key, value)
		}
	}

	return c.doWithRetry(req)
}

// doRequest is a helper method for POST/PUT requests
func (c *Client) doRequest(ctx context.Context, method, url string, body any, headers ...map[string]string) (*Response, error) {
	var bodyReader io.Reader

	if body != nil {
		switch v := body.(type) {
		case string:
			bodyReader = strings.NewReader(v)
		case []byte:
			bodyReader = bytes.NewReader(v)
		case io.Reader:
			bodyReader = v
		default:
			// Assume JSON serializable
			jsonBytes, err := json.Marshal(body)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal body to JSON: %w", err)
			}
			bodyReader = bytes.NewReader(jsonBytes)
		}
	}

	req, err := c.buildRequest(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to build %s request: %w", method, err)
	}

	// Set content type for JSON if not specified
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	// Add custom headers
	for _, headerMap := range headers {
		for key, value := range headerMap {
			req.Header.Set(key, value)
		}
	}

	return c.doWithRetry(req)
}

// JSON unmarshals the response body into the provided interface
func (r *Response) JSON(v any) error {
	if r.Body == nil {
		return fmt.Errorf("response body is nil")
	}

	return json.Unmarshal(r.Body, v)
}

// String returns the response body as a string
func (r *Response) String() string {
	if r.Body == nil {
		return ""
	}
	return string(r.Body)
}

// IsSuccess returns true if the status code indicates success (2xx)
func (r *Response) IsSuccess() bool {
	return r.StatusCode >= 200 && r.StatusCode < 300
}

// IsError returns true if the status code indicates an error (4xx or 5xx)
func (r *Response) IsError() bool {
	return r.StatusCode >= 400
}

// Close closes the underlying HTTP client
func (c *Client) Close() {
	if transport, ok := c.client.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
}
