package kafka

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"go-kafka-eda-demo/pkg/config"
	"go-kafka-eda-demo/pkg/logger"
)

type SchemaRegistryClient struct {
	baseURL    string
	httpClient *http.Client
}

type Schema struct {
	ID      int    `json:"id"`
	Version int    `json:"version"`
	Schema  string `json:"schema"`
	Subject string `json:"subject"`
}

type SchemaRegistryResponse struct {
	ID int `json:"id"`
}

type SubjectVersionResponse struct {
	Subject string `json:"subject"`
	Version int    `json:"version"`
	ID      int    `json:"id"`
	Schema  string `json:"schema"`
}

type CompatibilityResponse struct {
	IsCompatible bool `json:"is_compatible"`
}

func NewSchemaRegistryClient(cfg *config.SchemaRegistryConfig) *SchemaRegistryClient {
	return &SchemaRegistryClient{
		baseURL: cfg.URL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *SchemaRegistryClient) RegisterSchema(ctx context.Context, subject, schema string) (int, error) {
	ctx, span := telemetry.StartSpan(ctx, "register_schema")
	defer span.End()

	url := fmt.Sprintf("%s/subjects/%s/versions", c.baseURL, subject)
	
	payload := map[string]string{
		"schema": schema,
	}
	
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/vnd.schemaregistry.v1+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("schema registry error: %s", string(body))
	}

	var response SchemaRegistryResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return 0, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	logger.Infof(ctx, "Registered schema for subject %s with ID %d", subject, response.ID)
	return response.ID, nil
}

func (c *SchemaRegistryClient) GetSchema(ctx context.Context, id int) (*Schema, error) {
	ctx, span := telemetry.StartSpan(ctx, "get_schema")
	defer span.End()

	url := fmt.Sprintf("%s/schemas/ids/%d", c.baseURL, id)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("schema registry error: %s", string(body))
	}

	var schema Schema
	if err := json.Unmarshal(body, &schema); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	schema.ID = id
	return &schema, nil
}

func (c *SchemaRegistryClient) GetLatestSchema(ctx context.Context, subject string) (*Schema, error) {
	ctx, span := telemetry.StartSpan(ctx, "get_latest_schema")
	defer span.End()

	url := fmt.Sprintf("%s/subjects/%s/versions/latest", c.baseURL, subject)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("schema registry error: %s", string(body))
	}

	var response SubjectVersionResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &Schema{
		ID:      response.ID,
		Version: response.Version,
		Schema:  response.Schema,
		Subject: response.Subject,
	}, nil
}

func (c *SchemaRegistryClient) GetSchemaByVersion(ctx context.Context, subject string, version int) (*Schema, error) {
	ctx, span := telemetry.StartSpan(ctx, "get_schema_by_version")
	defer span.End()

	url := fmt.Sprintf("%s/subjects/%s/versions/%d", c.baseURL, subject, version)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("schema registry error: %s", string(body))
	}

	var response SubjectVersionResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &Schema{
		ID:      response.ID,
		Version: response.Version,
		Schema:  response.Schema,
		Subject: response.Subject,
	}, nil
}

func (c *SchemaRegistryClient) CheckCompatibility(ctx context.Context, subject, schema string) (bool, error) {
	ctx, span := telemetry.StartSpan(ctx, "check_compatibility")
	defer span.End()

	url := fmt.Sprintf("%s/compatibility/subjects/%s/versions/latest", c.baseURL, subject)
	
	payload := map[string]string{
		"schema": schema,
	}
	
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/vnd.schemaregistry.v1+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("schema registry error: %s", string(body))
	}

	var response CompatibilityResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return false, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	logger.Infof(ctx, "Compatibility check for subject %s: %v", subject, response.IsCompatible)
	return response.IsCompatible, nil
}

func (c *SchemaRegistryClient) ListSubjects(ctx context.Context) ([]string, error) {
	ctx, span := telemetry.StartSpan(ctx, "list_subjects")
	defer span.End()

	url := fmt.Sprintf("%s/subjects", c.baseURL)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("schema registry error: %s", string(body))
	}

	var subjects []string
	if err := json.Unmarshal(body, &subjects); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return subjects, nil
}

func (c *SchemaRegistryClient) GetSubjectVersions(ctx context.Context, subject string) ([]int, error) {
	ctx, span := telemetry.StartSpan(ctx, "get_subject_versions")
	defer span.End()

	url := fmt.Sprintf("%s/subjects/%s/versions", c.baseURL, subject)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("schema registry error: %s", string(body))
	}

	var versions []int
	if err := json.Unmarshal(body, &versions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return versions, nil
}

// AvroSerializer handles Avro serialization with schema registry
type AvroSerializer struct {
	schemaRegistry *SchemaRegistryClient
	schemaCache    map[int]string
}

func NewAvroSerializer(schemaRegistry *SchemaRegistryClient) *AvroSerializer {
	return &AvroSerializer{
		schemaRegistry: schemaRegistry,
		schemaCache:    make(map[int]string),
	}
}

func (s *AvroSerializer) Serialize(ctx context.Context, subject string, data interface{}) ([]byte, error) {
	ctx, span := telemetry.StartSpan(ctx, "avro_serialize")
	defer span.End()

	// Get latest schema for subject
	schema, err := s.schemaRegistry.GetLatestSchema(ctx, subject)
	if err != nil {
		return nil, fmt.Errorf("failed to get schema: %w", err)
	}

	// In a real implementation, you would use the Avro library to serialize
	// For this demo, we'll use JSON serialization with schema ID prefix
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}

	// Confluent wire format: magic byte (0) + schema ID (4 bytes) + data
	result := make([]byte, 5+len(jsonData))
	result[0] = 0 // Magic byte
	
	// Schema ID in big-endian format
	result[1] = byte(schema.ID >> 24)
	result[2] = byte(schema.ID >> 16)
	result[3] = byte(schema.ID >> 8)
	result[4] = byte(schema.ID)
	
	copy(result[5:], jsonData)

	logger.Debugf(ctx, "Serialized data with schema ID %d for subject %s", schema.ID, subject)
	return result, nil
}

func (s *AvroSerializer) Deserialize(ctx context.Context, data []byte) (interface{}, error) {
	ctx, span := telemetry.StartSpan(ctx, "avro_deserialize")
	defer span.End()

	if len(data) < 5 {
		return nil, fmt.Errorf("data too short for Confluent wire format")
	}

	if data[0] != 0 {
		return nil, fmt.Errorf("invalid magic byte")
	}

	// Extract schema ID
	schemaID := int(data[1])<<24 | int(data[2])<<16 | int(data[3])<<8 | int(data[4])

	// Get schema from cache or registry
	schemaStr, exists := s.schemaCache[schemaID]
	if !exists {
		schema, err := s.schemaRegistry.GetSchema(ctx, schemaID)
		if err != nil {
			return nil, fmt.Errorf("failed to get schema %d: %w", schemaID, err)
		}
		schemaStr = schema.Schema
		s.schemaCache[schemaID] = schemaStr
	}

	// In a real implementation, you would use the Avro library to deserialize
	// For this demo, we'll use JSON deserialization
	var result interface{}
	if err := json.Unmarshal(data[5:], &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal data: %w", err)
	}

	logger.Debugf(ctx, "Deserialized data with schema ID %d", schemaID)
	return result, nil
}

