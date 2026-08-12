resource "aws_ecs_task_definition" "api" {
  container_definitions = jsonencode([{
    essential = true
    image     = "453722413624.dkr.ecr.eu-west-2.amazonaws.com/accountable@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    mountPoints = [{
      containerPath = "/var/lib/accountable"
      readOnly      = false
      sourceVolume  = "host-scratch"
    }]
    name = "api"
  }])
}
