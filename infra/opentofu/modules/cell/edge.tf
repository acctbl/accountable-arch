resource "aws_cloudfront_origin_access_control" "web" {
  name                              = local.name
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

resource "aws_cloudfront_vpc_origin" "api" {
  vpc_origin_endpoint_config {
    arn                    = aws_lb.api.arn
    http_port              = 80
    https_port             = 443
    name                   = local.name
    origin_protocol_policy = "http-only"

    origin_ssl_protocols {
      items    = ["TLSv1.2"]
      quantity = 1
    }
  }
}

resource "aws_cloudfront_function" "api_prefix" {
  code    = file("${path.module}/functions/api-prefix.js")
  comment = "Removes the public API prefix before forwarding to Connect"
  name    = "${local.name}-api-prefix"
  publish = true
  runtime = "cloudfront-js-2.0"
}

resource "aws_cloudfront_function" "spa_router" {
  code    = file("${path.module}/functions/spa-router.js")
  comment = "Routes extensionless document requests to the SPA shell"
  name    = "${local.name}-spa-router"
  publish = true
  runtime = "cloudfront-js-2.0"
}

resource "aws_cloudfront_cache_policy" "documents" {
  name        = "${local.name}-documents"
  default_ttl = 0
  max_ttl     = 0
  min_ttl     = 0

  parameters_in_cache_key_and_forwarded_to_origin {
    cookies_config {
      cookie_behavior = "none"
    }

    headers_config {
      header_behavior = "none"
    }

    query_strings_config {
      query_string_behavior = "none"
    }
  }
}

resource "aws_cloudfront_cache_policy" "assets" {
  name        = "${local.name}-assets"
  default_ttl = 86400
  max_ttl     = 31536000
  min_ttl     = 1

  parameters_in_cache_key_and_forwarded_to_origin {
    enable_accept_encoding_brotli = true
    enable_accept_encoding_gzip   = true

    cookies_config {
      cookie_behavior = "none"
    }

    headers_config {
      header_behavior = "none"
    }

    query_strings_config {
      query_string_behavior = "none"
    }
  }
}

resource "aws_cloudfront_cache_policy" "api" {
  name        = "${local.name}-api"
  default_ttl = 0
  max_ttl     = 0
  min_ttl     = 0

  parameters_in_cache_key_and_forwarded_to_origin {
    cookies_config {
      cookie_behavior = "none"
    }

    headers_config {
      header_behavior = "none"
    }

    query_strings_config {
      query_string_behavior = "none"
    }
  }
}

resource "aws_cloudfront_origin_request_policy" "api" {
  name = "${local.name}-api"

  cookies_config {
    cookie_behavior = "all"
  }

  headers_config {
    header_behavior = "allExcept"

    headers {
      items = ["host"]
    }
  }

  query_strings_config {
    query_string_behavior = "all"
  }
}

resource "aws_cloudfront_origin_request_policy" "client_telemetry" {
  name = "${local.name}-client-telemetry"

  cookies_config {
    cookie_behavior = "none"
  }

  headers_config {
    header_behavior = "whitelist"

    headers {
      items = [
        "Content-Type",
        "Origin",
        "X-Correlation-ID",
        "X-Accountable-Proof-Timestamp",
        "X-Accountable-Proof-Nonce",
        "X-Accountable-Proof-Signature",
      ]
    }
  }

  query_strings_config {
    query_string_behavior = "none"
  }
}

resource "aws_cloudfront_response_headers_policy" "documents" {
  name = "${local.name}-documents"

  custom_headers_config {
    items {
      header   = "Cache-Control"
      override = true
      value    = "no-cache"
    }
  }

  security_headers_config {
    # A static header cannot contain a nonce. The hash pins the inline
    # document bootstrap script from apps/web/src/document-bootstrap.ts.
    # Keep content-security-policy.txt in sync with:
    #   pnpm --filter @accountable/web csp:write
    content_security_policy {
      override                = true
      content_security_policy = trimspace(file("${path.module}/content-security-policy.txt"))
    }

    content_type_options {
      override = true
    }

    frame_options {
      frame_option = "DENY"
      override     = true
    }

    referrer_policy {
      override        = true
      referrer_policy = "strict-origin-when-cross-origin"
    }

    strict_transport_security {
      access_control_max_age_sec = 31536000
      include_subdomains         = true
      override                   = true
      preload                    = true
    }
  }
}

resource "aws_cloudfront_response_headers_policy" "runtime_config" {
  name = "${local.name}-runtime-config"

  custom_headers_config {
    items {
      header   = "Cache-Control"
      override = true
      value    = "no-store"
    }
  }
}

resource "aws_cloudfront_response_headers_policy" "assets" {
  name = "${local.name}-assets"

  custom_headers_config {
    items {
      header   = "Cache-Control"
      override = true
      value    = "public, max-age=31536000, immutable"
    }
  }
}

resource "aws_wafv2_web_acl" "edge" {
  provider = aws.global

  name  = local.name
  scope = "CLOUDFRONT"

  default_action {
    allow {}
  }

  rule {
    name     = "ClientTelemetryRateLimit"
    priority = 25

    action {
      block {}
    }

    statement {
      rate_based_statement {
        aggregate_key_type    = "IP"
        evaluation_window_sec = 300
        limit                 = 600

        scope_down_statement {
          byte_match_statement {
            positional_constraint = "EXACTLY"
            search_string         = "/api/telemetry/v1"

            field_to_match {
              uri_path {}
            }

            text_transformation {
              priority = 0
              type     = "NONE"
            }
          }
        }
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${local.name}-client-telemetry-rate-limit"
      sampled_requests_enabled   = false
    }
  }

  rule {
    name     = "AWSManagedRulesCommonRuleSet"
    priority = 10

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesCommonRuleSet"
        vendor_name = "AWS"

        rule_action_override {
          name = "SizeRestrictions_BODY"

          action_to_use {
            count {}
          }
        }
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${local.name}-common"
      sampled_requests_enabled   = true
    }
  }

  rule {
    name     = "AWSManagedRulesKnownBadInputsRuleSet"
    priority = 20

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesKnownBadInputsRuleSet"
        vendor_name = "AWS"
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${local.name}-known-bad-inputs"
      sampled_requests_enabled   = true
    }
  }

  rule {
    name     = "IpRateLimit"
    priority = 30

    action {
      block {}
    }

    statement {
      rate_based_statement {
        aggregate_key_type    = "IP"
        evaluation_window_sec = 300
        limit                 = 2000
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${local.name}-ip-rate-limit"
      sampled_requests_enabled   = true
    }
  }

  visibility_config {
    cloudwatch_metrics_enabled = true
    metric_name                = local.name
    sampled_requests_enabled   = true
  }
}

resource "aws_cloudfront_distribution" "cell" {
  comment             = local.name
  default_root_object = "index.html"
  enabled             = true
  http_version        = "http2and3"
  is_ipv6_enabled     = true
  price_class         = "PriceClass_100"
  web_acl_id          = aws_wafv2_web_acl.edge.arn

  origin {
    domain_name              = aws_s3_bucket.web.bucket_regional_domain_name
    origin_access_control_id = aws_cloudfront_origin_access_control.web.id
    origin_id                = "web"
  }

  origin {
    domain_name = aws_lb.api.dns_name
    origin_id   = "api"

    vpc_origin_config {
      vpc_origin_id = aws_cloudfront_vpc_origin.api.id
    }
  }

  default_cache_behavior {
    allowed_methods            = ["GET", "HEAD"]
    cache_policy_id            = aws_cloudfront_cache_policy.documents.id
    cached_methods             = ["GET", "HEAD"]
    compress                   = true
    response_headers_policy_id = aws_cloudfront_response_headers_policy.documents.id
    target_origin_id           = "web"
    viewer_protocol_policy     = "redirect-to-https"

    function_association {
      event_type   = "viewer-request"
      function_arn = aws_cloudfront_function.spa_router.arn
    }
  }

  ordered_cache_behavior {
    allowed_methods          = ["OPTIONS", "POST"]
    cache_policy_id          = aws_cloudfront_cache_policy.api.id
    cached_methods           = []
    compress                 = false
    origin_request_policy_id = aws_cloudfront_origin_request_policy.client_telemetry.id
    path_pattern             = "/api/telemetry/v1"
    target_origin_id         = "api"
    viewer_protocol_policy   = "redirect-to-https"

    function_association {
      event_type   = "viewer-request"
      function_arn = aws_cloudfront_function.api_prefix.arn
    }
  }

  ordered_cache_behavior {
    allowed_methods            = ["GET", "HEAD"]
    cache_policy_id            = aws_cloudfront_cache_policy.documents.id
    cached_methods             = ["GET", "HEAD"]
    compress                   = true
    path_pattern               = "/_runtime/config.json"
    response_headers_policy_id = aws_cloudfront_response_headers_policy.runtime_config.id
    target_origin_id           = "web"
    viewer_protocol_policy     = "redirect-to-https"
  }

  ordered_cache_behavior {
    allowed_methods            = ["GET", "HEAD"]
    cache_policy_id            = aws_cloudfront_cache_policy.assets.id
    cached_methods             = ["GET", "HEAD"]
    compress                   = true
    path_pattern               = "/assets/*"
    response_headers_policy_id = aws_cloudfront_response_headers_policy.assets.id
    target_origin_id           = "web"
    viewer_protocol_policy     = "redirect-to-https"
  }

  ordered_cache_behavior {
    allowed_methods          = ["DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT"]
    cache_policy_id          = aws_cloudfront_cache_policy.api.id
    cached_methods           = ["GET", "HEAD"]
    compress                 = true
    origin_request_policy_id = aws_cloudfront_origin_request_policy.api.id
    path_pattern             = "/api/*"
    target_origin_id         = "api"
    viewer_protocol_policy   = "redirect-to-https"

    function_association {
      event_type   = "viewer-request"
      function_arn = aws_cloudfront_function.api_prefix.arn
    }
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  # The default certificate allows no other field in this block.
  # AWS sets the security policy to TLSv1 and removes a stronger value.
  # A stronger value therefore causes a permanent plan difference.
  # TLS 1.2 needs a custom hostname and an ACM certificate in us-east-1.
  viewer_certificate {
    cloudfront_default_certificate = true
  }
}
