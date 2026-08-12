resource "aws_ecs_task_definition" "api" {
  container_definitions = jsonencode([{
    essential = true
    image     = "453722413624.dkr.ecr.eu-west-2.amazonaws.com/accountable@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    linuxParameters = {
      tmpfs = [{
        containerPath = "/run/accountable"
        mountOptions  = ["rw", "noexec", "nosuid", "nodev", "uid=65532", "gid=65532", "mode=0700"]
        size          = 64
      }]
    }
    name = "api"
  }])
}
