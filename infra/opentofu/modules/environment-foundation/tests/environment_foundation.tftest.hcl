mock_provider "aws" {
  alias = "mock"

  mock_data "aws_iam_policy_document" {
    defaults = {
      json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}"
    }
  }

  mock_data "aws_caller_identity" {
    defaults = {
      account_id = "453722413624"
    }
  }

  mock_resource "aws_iam_openid_connect_provider" {
    defaults = {
      arn = "arn:aws:iam::453722413624:oidc-provider/token.actions.githubusercontent.com"
    }
  }

  mock_resource "aws_iam_role" {
    defaults = {
      arn = "arn:aws:iam::453722413624:role/accountable"
    }
  }

  mock_resource "aws_iam_policy" {
    defaults = {
      arn = "arn:aws:iam::453722413624:policy/accountable"
    }
  }
}

variables {
  account_id                 = "453722413624"
  environment                = "development"
  github_oidc_subject_prefix = "repo:acctbl@309473689/accountable@1318144297"
  github_repository          = "acctbl/accountable"
  region                     = "eu-west-2"
}

run "repository_is_immutable_and_scanned" {
  command = plan

  providers = {
    aws = aws.mock
  }

  assert {
    condition     = aws_ecr_repository.application.image_tag_mutability == "IMMUTABLE"
    error_message = "The application repository must reject mutable tags."
  }

  assert {
    condition     = aws_ecr_repository.application.image_scanning_configuration[0].scan_on_push
    error_message = "The application repository must scan images on push."
  }

  assert {
    condition = (
      aws_ecr_repository.synthetic.image_tag_mutability == "IMMUTABLE" &&
      aws_ecr_repository.synthetic.image_scanning_configuration[0].scan_on_push
    )
    error_message = "The synthetic workload repository must be immutable and scanned."
  }
}
