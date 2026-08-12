package telemetry

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"strconv"
	"time"
)

var ErrInvalidContract = errors.New("invalid telemetry contract")

type clientFieldRule struct {
	Kind          string
	Pattern       string
	MaxProperties int
	ConstInteger  *int
}

type clientRegistryDefinition struct {
	SchemaVersion       int                     `json:"schema_version"`
	SamplingAlgorithm   string                  `json:"sampling_algorithm"`
	ClientApplications  []string                `json:"client_applications"`
	ClientArtifactTypes []string                `json:"client_artifact_types"`
	RouteClasses        []string                `json:"route_classes"`
	Events              []clientEventDefinition `json:"events"`
}

type clientEventDefinition struct {
	Name               string                         `json:"name"`
	SampleBasisPoints  int                            `json:"sample_basis_points"`
	EmissionGroup      string                         `json:"emission_group"`
	DocumentLimit      int                            `json:"document_limit"`
	Attributes         map[string]clientAttributeRule `json:"attributes"`
	RequiredAttributes []string                       `json:"required_attributes"`
}

type clientAttributeRule struct {
	Type    string   `json:"type"`
	Minimum int64    `json:"minimum"`
	Maximum int64    `json:"maximum"`
	Values  []string `json:"values"`
}

type ClientIdentityTuple struct {
	Application                string
	ReleaseID                  string
	ArtifactType               string
	ArtifactDigest             string
	ConfigurationSchemaVersion int
	ConfigurationRevision      string
	ConfigurationFingerprint   string
}

type ClientAdmissionPolicy struct {
	Now               time.Time
	ActiveIdentity    ClientIdentityTuple
	PreviousIdentity  *ClientIdentityTuple
	MaximumPastAge    time.Duration
	MaximumFutureSkew time.Duration
}

var (
	clientRegistry      = loadClientRegistry()
	clientFieldPatterns = compileClientFieldPatterns()
	clientEvents        = indexClientEvents()
)

func ParseClientEnvelope(raw []byte) (ClientEnvelope, error) {
	value, err := decodeJSONObject(raw)
	if err != nil {
		return ClientEnvelope{}, err
	}
	if err := validateGeneratedClientFields(value); err != nil {
		return ClientEnvelope{}, err
	}
	if err := validateClientRegistryFields(value); err != nil {
		return ClientEnvelope{}, err
	}
	var envelope ClientEnvelope
	if err := decodeStrictJSON(raw, &envelope); err != nil {
		return ClientEnvelope{}, err
	}
	return envelope, nil
}

func ValidateClientAdmission(envelope ClientEnvelope, policy ClientAdmissionPolicy) error {
	pastAge := policy.MaximumPastAge
	if pastAge == 0 {
		pastAge = 5 * time.Minute
	}
	futureSkew := policy.MaximumFutureSkew
	if futureSkew == 0 {
		futureSkew = time.Minute
	}
	occurredAt, err := time.Parse("2006-01-02T15:04:05.000Z", envelope.OccurredAt)
	if err != nil {
		return contractError("occurred_at")
	}
	now := policy.Now.UTC()
	if occurredAt.Before(now.Add(-pastAge)) || occurredAt.After(now.Add(futureSkew)) {
		return contractError("occurred_at")
	}
	sampled, err := ClientSampled(envelope.CorrelationID, envelope.EventName)
	if err != nil || !sampled {
		return contractError("client sampling")
	}
	identity := ClientIdentityTuple{
		Application:                envelope.ClientApplication,
		ReleaseID:                  envelope.ReleaseID,
		ArtifactType:               envelope.ClientArtifactType,
		ArtifactDigest:             envelope.ClientArtifactDigest,
		ConfigurationSchemaVersion: envelope.ConfigurationSchemaVersion,
		ConfigurationRevision:      envelope.ConfigurationRevision,
		ConfigurationFingerprint:   envelope.ConfigurationFingerprint,
	}
	if identity == policy.ActiveIdentity {
		return nil
	}
	if policy.PreviousIdentity != nil && identity == *policy.PreviousIdentity {
		return nil
	}
	return contractError("client identity tuple")
}

func ValidateClientIdentityTuple(identity ClientIdentityTuple) error {
	if !contains(clientRegistry.ClientApplications, identity.Application) ||
		!uuidV7Pattern.MatchString(identity.ReleaseID) ||
		!contains(clientRegistry.ClientArtifactTypes, identity.ArtifactType) ||
		!digestPattern.MatchString(identity.ArtifactDigest) ||
		identity.ConfigurationSchemaVersion != ContractSchemaVersion ||
		!revisionPattern.MatchString(identity.ConfigurationRevision) ||
		!fingerprintPattern.MatchString(identity.ConfigurationFingerprint) {
		return contractError("client identity tuple")
	}
	return nil
}

func ClientSampled(correlationID, eventName string) (bool, error) {
	event, ok := clientEvents[eventName]
	if !ok || !clientFieldPatterns["correlation_id"].MatchString(correlationID) {
		return false, contractError("sampling input")
	}
	digest := sha256.Sum256([]byte(correlationID + "\n" + eventName))
	bucket := binary.BigEndian.Uint32(digest[:4]) % 10000
	return bucket < uint32(event.SampleBasisPoints), nil
}

func intPointer(value int) *int {
	return &value
}

func loadClientRegistry() clientRegistryDefinition {
	var registry clientRegistryDefinition
	if err := json.Unmarshal([]byte(generatedClientRegistryJSON), &registry); err != nil {
		panic(err)
	}
	return registry
}

func compileClientFieldPatterns() map[string]*regexp.Regexp {
	patterns := make(map[string]*regexp.Regexp)
	for name, rule := range generatedClientFieldRules {
		if rule.Pattern != "" {
			patterns[name] = regexp.MustCompile(rule.Pattern)
		}
	}
	return patterns
}

func indexClientEvents() map[string]clientEventDefinition {
	events := make(map[string]clientEventDefinition, len(clientRegistry.Events))
	for _, event := range clientRegistry.Events {
		events[event.Name] = event
	}
	return events
}

func validateGeneratedClientFields(value map[string]any) error {
	if len(value) != len(generatedClientFieldRules) {
		return contractError("client envelope fields")
	}
	for name, rule := range generatedClientFieldRules {
		field, ok := value[name]
		if !ok {
			return contractError(name)
		}
		if err := validateClientField(name, field, rule); err != nil {
			return err
		}
	}
	return nil
}

func validateClientField(name string, value any, rule clientFieldRule) error {
	switch rule.Kind {
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return contractError(name)
		}
		parsed, err := strconv.ParseInt(number.String(), 10, 64)
		if err != nil {
			return contractError(name)
		}
		if rule.ConstInteger != nil && parsed != int64(*rule.ConstInteger) {
			return contractError(name)
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return contractError(name)
		}
		if pattern := clientFieldPatterns[name]; pattern != nil && !pattern.MatchString(text) {
			return contractError(name)
		}
	case "object":
		object, ok := value.(map[string]any)
		if !ok || len(object) > rule.MaxProperties {
			return contractError(name)
		}
		for _, attribute := range object {
			switch attribute.(type) {
			case string, bool, json.Number:
			default:
				return contractError(name)
			}
		}
	default:
		return contractError(name)
	}
	return nil
}

func validateClientRegistryFields(value map[string]any) error {
	occurredAt := value["occurred_at"].(string)
	parsedOccurredAt, err := time.Parse("2006-01-02T15:04:05.000Z", occurredAt)
	if err != nil || parsedOccurredAt.Format("2006-01-02T15:04:05.000Z") != occurredAt {
		return contractError("occurred_at")
	}
	if !contains(clientRegistry.ClientApplications, value["client_application"].(string)) {
		return contractError("client_application")
	}
	if !contains(clientRegistry.ClientArtifactTypes, value["client_artifact_type"].(string)) {
		return contractError("client_artifact_type")
	}
	if !contains(clientRegistry.RouteClasses, value["route_class"].(string)) {
		return contractError("route_class")
	}
	event, ok := clientEvents[value["event_name"].(string)]
	if !ok {
		return contractError("event_name")
	}
	attributes := value["attributes"].(map[string]any)
	if len(attributes) != len(event.RequiredAttributes) {
		return contractError("attributes")
	}
	for _, name := range event.RequiredAttributes {
		attribute, ok := attributes[name]
		if !ok {
			return contractError("attributes")
		}
		if err := validateClientAttribute(attribute, event.Attributes[name]); err != nil {
			return err
		}
	}
	if event.Name == "api.request.completed" {
		statusClass := attributes["status_class"].(string)
		outcome := attributes["outcome"].(string)
		if (statusClass == "5xx" || statusClass == "network") != (outcome == "failure") {
			return contractError("outcome")
		}
	}
	return nil
}

func validateClientAttribute(value any, rule clientAttributeRule) error {
	switch rule.Type {
	case "string":
		text, ok := value.(string)
		if !ok || !contains(rule.Values, text) {
			return contractError("attribute")
		}
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return contractError("attribute")
		}
		integer, err := strconv.ParseInt(number.String(), 10, 64)
		if err != nil || integer < rule.Minimum || integer > rule.Maximum {
			return contractError("attribute")
		}
	case "number":
		number, ok := value.(json.Number)
		if !ok {
			return contractError("attribute")
		}
		parsed, ok := new(big.Rat).SetString(number.String())
		if !ok || parsed.Cmp(big.NewRat(rule.Minimum, 1)) < 0 || parsed.Cmp(big.NewRat(rule.Maximum, 1)) > 0 {
			return contractError("attribute")
		}
	default:
		return contractError("attribute")
	}
	return nil
}

func decodeJSONObject(raw []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, contractError("json")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, contractError("json")
	}
	return value, nil
}

func decodeStrictJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return contractError("json")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return contractError("json")
	}
	return nil
}

func contains(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func contractError(field string) error {
	return fmt.Errorf("%w: %s", ErrInvalidContract, field)
}
