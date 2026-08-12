package tofupolicy

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	accountIDPattern = regexp.MustCompile(`^[0-9]{12}$`)
	imageURIpattern  = regexp.MustCompile(`^[^[:space:]@]+@sha256:[a-f0-9]{64}$`)
)

type Violation struct {
	Address string
	Message string
}

type planDocument struct {
	Configuration struct {
		ProviderConfig map[string]providerConfiguration `json:"provider_config"`
		RootModule     configurationModule              `json:"root_module"`
	} `json:"configuration"`
	PlannedValues struct {
		RootModule plannedModule `json:"root_module"`
	} `json:"planned_values"`
	ResourceChanges []resourceChange        `json:"resource_changes"`
	Variables       map[string]planVariable `json:"variables"`
}

type planVariable struct {
	Value any `json:"value"`
}

type providerConfiguration struct {
	Expressions map[string]json.RawMessage `json:"expressions"`
}

type expression struct {
	ConstantValue any      `json:"constant_value"`
	References    []string `json:"references"`
}

type configurationModule struct {
	Resources   []configurationResource            `json:"resources"`
	ModuleCalls map[string]configurationModuleCall `json:"module_calls"`
}

type configurationModuleCall struct {
	Expressions map[string]expression `json:"expressions"`
	Module      configurationModule   `json:"module"`
}

type configurationResource struct {
	Address     string                     `json:"address"`
	Type        string                     `json:"type"`
	Expressions map[string]json.RawMessage `json:"expressions"`
}

type plannedModule struct {
	Resources    []plannedResource `json:"resources"`
	ChildModules []plannedModule   `json:"child_modules"`
}

type plannedResource struct {
	Address string         `json:"address"`
	Type    string         `json:"type"`
	Values  map[string]any `json:"values"`
	Unknown map[string]any `json:"-"`
}

type resourceChange struct {
	Address string `json:"address"`
	Type    string `json:"type"`
	Change  struct {
		After        map[string]any `json:"after"`
		AfterUnknown map[string]any `json:"after_unknown"`
	} `json:"change"`
}

func Evaluate(payload []byte, expectedAccountID string) ([]Violation, error) {
	if !accountIDPattern.MatchString(expectedAccountID) {
		return nil, errors.New("expected account ID must contain 12 digits")
	}
	var plan planDocument
	if err := json.Unmarshal(payload, &plan); err != nil {
		return nil, fmt.Errorf("decode saved plan JSON: %w", err)
	}

	violations := accountViolations(plan.Configuration.ProviderConfig, expectedAccountID)
	resources := flattenPlannedResources(plan.PlannedValues.RootModule)
	if len(resources) == 0 {
		resources = changedResources(plan.ResourceChanges)
	} else {
		unknownByAddress := make(map[string]map[string]any, len(plan.ResourceChanges))
		for _, change := range plan.ResourceChanges {
			unknownByAddress[change.Address] = change.Change.AfterUnknown
		}
		for index := range resources {
			resources[index].Unknown = unknownByAddress[resources[index].Address]
		}
	}
	violations = append(violations, resourceViolations(resources, taskImageProofs(plan), s3PublicAccessBlockTargets(plan))...)
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Address == violations[j].Address {
			return violations[i].Message < violations[j].Message
		}
		return violations[i].Address < violations[j].Address
	})
	return violations, nil
}

func accountViolations(configurations map[string]providerConfiguration, expected string) []Violation {
	if _, ok := configurations["aws"]; !ok {
		return []Violation{{Address: "provider.aws", Message: "default AWS provider does not prove the expected account"}}
	}
	var violations []Violation
	for name, provider := range configurations {
		if name != "aws" && !strings.HasPrefix(name, "aws.") {
			continue
		}
		rawValue, ok := provider.Expressions["allowed_account_ids"]
		if !ok {
			violations = append(violations, Violation{Address: "provider." + name, Message: "AWS provider has no account allow-list"})
			continue
		}
		var value expression
		if err := json.Unmarshal(rawValue, &value); err != nil {
			violations = append(violations, Violation{Address: "provider." + name, Message: "AWS provider account allow-list cannot be proven"})
			continue
		}
		accounts, ok := value.ConstantValue.([]any)
		if !ok || len(accounts) != 1 || accounts[0] != expected {
			violations = append(violations, Violation{Address: "provider." + name, Message: "AWS provider account allow-list does not match the expected account"})
		}
	}
	return violations
}

func flattenPlannedResources(module plannedModule) []plannedResource {
	resources := append([]plannedResource(nil), module.Resources...)
	for _, child := range module.ChildModules {
		resources = append(resources, flattenPlannedResources(child)...)
	}
	return resources
}

func changedResources(changes []resourceChange) []plannedResource {
	resources := make([]plannedResource, 0, len(changes))
	for _, change := range changes {
		if change.Change.After == nil {
			continue
		}
		resources = append(resources, plannedResource{
			Address: change.Address,
			Type:    change.Type,
			Values:  change.Change.After,
			Unknown: change.Change.AfterUnknown,
		})
	}
	return resources
}

func resourceViolations(resources []plannedResource, imageProofs map[string]bool, s3BlockTargets map[string]string) []Violation {
	var violations []Violation
	buckets := make(map[string]string)
	bucketAddresses := make(map[string]bool)
	publicAccessBlocks := make(map[string]bool)
	protectedBucketAddresses := make(map[string]bool)
	hasWAF := hasResourceType(resources, "aws_wafv2_web_acl")

	for _, resource := range resources {
		switch resource.Type {
		case "aws_db_instance":
			if !boolean(resource.Values, "multi_az") {
				violations = append(violations, violation(resource, "RDS must be Multi-AZ"))
			}
			if !explicitFalse(resource.Values, "publicly_accessible") {
				violations = append(violations, violation(resource, "RDS publicly_accessible must be false"))
			}
			if !boolean(resource.Values, "storage_encrypted") {
				violations = append(violations, violation(resource, "RDS storage must be encrypted"))
			}
		case "aws_lb":
			if !boolean(resource.Values, "internal") {
				violations = append(violations, violation(resource, "ALB must be internal"))
			}
		case "aws_ecs_service":
			if ecsAssignsPublicIP(resource.Values) {
				violations = append(violations, violation(resource, "ECS service must not assign a public IP"))
			}
		case "aws_vpc_security_group_ingress_rule":
			if worldOpen(resource.Values["cidr_ipv4"]) || worldOpen(resource.Values["cidr_ipv6"]) ||
				fieldIsUnknown(resource.Unknown, "cidr_ipv4") || fieldIsUnknown(resource.Unknown, "cidr_ipv6") {
				violations = append(violations, violation(resource, "security-group ingress is world-open"))
			}
		case "aws_security_group_rule":
			if resource.Values["type"] == "ingress" && (worldOpenList(resource.Values["cidr_blocks"]) || worldOpenList(resource.Values["ipv6_cidr_blocks"])) {
				violations = append(violations, violation(resource, "security-group ingress is world-open"))
			}
		case "aws_security_group":
			if inlineIngressIsWorldOpen(resource.Values["ingress"]) {
				violations = append(violations, violation(resource, "security-group ingress is world-open"))
			}
		case "aws_s3_bucket":
			bucketAddresses[resource.Address] = true
			if bucket, ok := resource.Values["bucket"].(string); ok && bucket != "" {
				buckets[bucket] = resource.Address
			}
		case "aws_s3_bucket_public_access_block":
			bucket, _ := resource.Values["bucket"].(string)
			complete := boolean(resource.Values, "block_public_acls") && boolean(resource.Values, "block_public_policy") &&
				boolean(resource.Values, "ignore_public_acls") && boolean(resource.Values, "restrict_public_buckets")
			if !complete {
				violations = append(violations, violation(resource, "S3 public-access controls must all be true"))
			}
			if bucket != "" {
				publicAccessBlocks[bucket] = complete
			}
			if complete {
				protectedBucketAddresses[s3BlockTargets[resource.Address]] = true
			}
		case "aws_ecs_task_definition":
			if !taskImagesUseDigests(resource.Values["container_definitions"]) &&
				(!fieldIsUnknown(resource.Unknown, "container_definitions") || !imageProofs[resource.Address]) {
				violations = append(violations, violation(resource, "every task image must use a sha256 digest"))
			}
			if taskUsesTmpfs(resource.Values["container_definitions"]) {
				violations = append(violations, violation(resource, "Fargate tasks must not use linuxParameters.tmpfs"))
			}
			if taskWritesOutsideTheScratchVolume(resource.Values["container_definitions"]) {
				violations = append(violations, violation(resource, "every container mount must be the task-scoped runtime scratch volume"))
			}
		case "aws_cloudfront_distribution":
			webACL, _ := resource.Values["web_acl_id"].(string)
			webACLUnknown, _ := resource.Unknown["web_acl_id"].(bool)
			if webACL == "" && (!webACLUnknown || !hasWAF) {
				violations = append(violations, violation(resource, "CloudFront must have an AWS WAF web ACL"))
			}
		case "aws_cloudfront_cache_policy":
			if cachePolicyDisablesCaching(resource.Values) && cachePolicyEnablesCompression(resource.Values) {
				violations = append(violations, violation(resource, "caching-disabled CloudFront cache policy cannot enable Accept-Encoding compression"))
			}
		}
	}

	for bucket, address := range buckets {
		if !publicAccessBlocks[bucket] && !protectedBucketAddresses[address] {
			violations = append(violations, Violation{Address: address, Message: "S3 bucket has no complete public-access block"})
		}
	}
	for address := range bucketAddresses {
		if _, hasKnownName := resourceValueAddress(buckets, address); !hasKnownName && !protectedBucketAddresses[address] {
			violations = append(violations, Violation{Address: address, Message: "S3 bucket has no complete public-access block"})
		}
	}
	return violations
}

func resourceValueAddress(values map[string]string, address string) (string, bool) {
	for value, candidate := range values {
		if candidate == address {
			return value, true
		}
	}
	return "", false
}

func taskImageProofs(plan planDocument) map[string]bool {
	proofs := make(map[string]bool)
	rootImageURI := ""
	if variable, ok := plan.Variables["image_uri"]; ok {
		rootImageURI, _ = variable.Value.(string)
	}
	walkConfigurationImages(plan.Configuration.RootModule, "", rootImageURI, proofs)
	return proofs
}

func walkConfigurationImages(module configurationModule, prefix, imageURI string, proofs map[string]bool) {
	for _, resource := range module.Resources {
		if resource.Type != "aws_ecs_task_definition" || !imageURIpattern.MatchString(imageURI) {
			continue
		}
		if expressionReferencesVariable(decodeExpression(resource.Expressions["container_definitions"]), "image_uri") {
			proofs[prefix+resource.Address] = true
		}
	}

	for name, call := range module.ModuleCalls {
		childImageURI := ""
		imageExpression, ok := call.Expressions["image_uri"]
		if ok {
			if constant, ok := imageExpression.ConstantValue.(string); ok {
				childImageURI = constant
			} else if expressionReferencesVariable(imageExpression, "image_uri") {
				childImageURI = imageURI
			}
		}
		walkConfigurationImages(call.Module, prefix+"module."+name+".", childImageURI, proofs)
	}
}

func expressionReferencesVariable(candidate expression, name string) bool {
	want := "var." + name
	for _, reference := range candidate.References {
		if reference == want {
			return true
		}
	}
	return false
}

func decodeExpression(payload json.RawMessage) expression {
	var decoded expression
	_ = json.Unmarshal(payload, &decoded)
	return decoded
}

func s3PublicAccessBlockTargets(plan planDocument) map[string]string {
	targets := make(map[string]string)
	walkConfigurationS3Blocks(plan.Configuration.RootModule, "", targets)
	return targets
}

func walkConfigurationS3Blocks(module configurationModule, prefix string, targets map[string]string) {
	for _, resource := range module.Resources {
		if resource.Type != "aws_s3_bucket_public_access_block" {
			continue
		}
		for _, reference := range decodeExpression(resource.Expressions["bucket"]).References {
			if bucketAddress, ok := referencedResourceAddress(reference, "aws_s3_bucket"); ok {
				targets[prefix+resource.Address] = prefix + bucketAddress
				break
			}
		}
	}
	for name, call := range module.ModuleCalls {
		walkConfigurationS3Blocks(call.Module, prefix+"module."+name+".", targets)
	}
}

func referencedResourceAddress(reference, resourceType string) (string, bool) {
	parts := strings.Split(reference, ".")
	if len(parts) < 2 || parts[0] != resourceType || parts[1] == "" {
		return "", false
	}
	return parts[0] + "." + parts[1], true
}

func violation(resource plannedResource, message string) Violation {
	return Violation{Address: resource.Address, Message: message}
}

func boolean(values map[string]any, field string) bool {
	value, _ := values[field].(bool)
	return value
}

func explicitFalse(values map[string]any, field string) bool {
	value, ok := values[field].(bool)
	return ok && !value
}

func fieldIsUnknown(values map[string]any, field string) bool {
	value, _ := values[field].(bool)
	return value
}

func ecsAssignsPublicIP(values map[string]any) bool {
	configurations, _ := values["network_configuration"].([]any)
	if len(configurations) != 1 {
		return true
	}
	configuration, _ := configurations[0].(map[string]any)
	return !explicitFalse(configuration, "assign_public_ip")
}

func hasResourceType(resources []plannedResource, resourceType string) bool {
	for _, resource := range resources {
		if resource.Type == resourceType {
			return true
		}
	}
	return false
}

func worldOpen(value any) bool {
	cidr, _ := value.(string)
	return cidr == "0.0.0.0/0" || cidr == "::/0"
}

func worldOpenList(value any) bool {
	values, _ := value.([]any)
	for _, candidate := range values {
		if worldOpen(candidate) {
			return true
		}
	}
	return false
}

func inlineIngressIsWorldOpen(value any) bool {
	rules, _ := value.([]any)
	for _, candidate := range rules {
		rule, _ := candidate.(map[string]any)
		if worldOpenList(rule["cidr_blocks"]) || worldOpenList(rule["ipv6_cidr_blocks"]) {
			return true
		}
	}
	return false
}

func taskImagesUseDigests(value any) bool {
	definitions, ok := value.(string)
	if !ok || definitions == "" {
		return false
	}
	var containers []struct {
		Image string `json:"image"`
	}
	if err := json.Unmarshal([]byte(definitions), &containers); err != nil || len(containers) == 0 {
		return false
	}
	for _, container := range containers {
		if !imageURIpattern.MatchString(container.Image) {
			return false
		}
	}
	return true
}

const scratchVolumeName = "runtime-scratch"

func decodeContainerDefinitions(value any) []containerDefinition {
	definitions, ok := value.(string)
	if !ok || definitions == "" {
		return nil
	}
	var containers []containerDefinition
	if err := json.Unmarshal([]byte(definitions), &containers); err != nil {
		return nil
	}
	return containers
}

type containerDefinition struct {
	LinuxParameters struct {
		Tmpfs []json.RawMessage `json:"tmpfs"`
	} `json:"linuxParameters"`
	MountPoints []struct {
		ContainerPath string `json:"containerPath"`
		SourceVolume  string `json:"sourceVolume"`
	} `json:"mountPoints"`
}

func taskUsesTmpfs(value any) bool {
	for _, container := range decodeContainerDefinitions(value) {
		if len(container.LinuxParameters.Tmpfs) > 0 {
			return true
		}
	}
	return false
}

func taskWritesOutsideTheScratchVolume(value any) bool {
	for _, container := range decodeContainerDefinitions(value) {
		for _, mount := range container.MountPoints {
			if mount.SourceVolume != scratchVolumeName || mount.ContainerPath != "/run/accountable" {
				return true
			}
		}
	}
	return false
}

func cachePolicyDisablesCaching(values map[string]any) bool {
	return numberIsZero(values["default_ttl"]) && numberIsZero(values["max_ttl"]) && numberIsZero(values["min_ttl"])
}

func cachePolicyEnablesCompression(values map[string]any) bool {
	blocks, _ := values["parameters_in_cache_key_and_forwarded_to_origin"].([]any)
	if len(blocks) == 0 {
		return false
	}
	block, _ := blocks[0].(map[string]any)
	return boolean(block, "enable_accept_encoding_gzip") || boolean(block, "enable_accept_encoding_brotli")
}

func numberIsZero(value any) bool {
	switch typed := value.(type) {
	case int:
		return typed == 0
	case int64:
		return typed == 0
	case json.Number:
		parsed, err := typed.Int64()
		return err == nil && parsed == 0
	default:
		// encoding/json decodes numbers into any as binary floating point;
		// compare via rendered text so we never name those types.
		switch fmt.Sprintf("%v", value) {
		case "0", "0.0":
			return true
		default:
			return false
		}
	}
}
