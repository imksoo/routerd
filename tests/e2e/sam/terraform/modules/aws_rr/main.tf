locals {
  common_tags = {
    ManagedBy         = var.managed_by
    Owner             = var.managed_by
    Project           = "routerd-sam-e2e"
    Environment       = "validation"
    ExpiresAt         = var.expires_at
    "routerd-purpose" = var.purpose
    "routerd-commit"  = var.commit
    "routerd-run-id"  = var.run_id
  }

  router_cloud_init = <<-CLOUD_INIT
    #cloud-config
    package_update: true
    packages: [curl, unzip]
    runcmd:
      - [bash, -lc, 'rm -rf /tmp/awscli-install /tmp/awscliv2.zip']
      - [bash, -lc, 'mkdir -p /tmp/awscli-install']
      - [bash, -lc, 'curl -fsSL https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip -o /tmp/awscliv2.zip']
      - [bash, -lc, 'unzip -q /tmp/awscliv2.zip -d /tmp/awscli-install']
      - [bash, -lc, '/tmp/awscli-install/aws/install --bin-dir /usr/local/bin --install-dir /usr/local/aws-cli --update']
      - [bash, -lc, 'aws --version']
      - [bash, -lc, 'rm -rf /tmp/awscli-install /tmp/awscliv2.zip']
  CLOUD_INIT
}

resource "aws_vpc" "lab" {
  cidr_block           = var.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = merge(local.common_tags, { Name = "routerd-${var.run_id}-aws" })
}

resource "aws_internet_gateway" "lab" {
  vpc_id = aws_vpc.lab.id
  tags   = merge(local.common_tags, { Name = "routerd-${var.run_id}-aws" })
}

resource "aws_subnet" "rr" {
  vpc_id                  = aws_vpc.lab.id
  cidr_block              = var.subnet_cidr
  map_public_ip_on_launch = true

  tags = merge(local.common_tags, { Name = "routerd-${var.run_id}-aws-rr" })
}

resource "aws_route_table" "rr" {
  vpc_id = aws_vpc.lab.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.lab.id
  }

  tags = merge(local.common_tags, { Name = "routerd-${var.run_id}-aws-rr" })
}

resource "aws_route_table_association" "rr" {
  subnet_id      = aws_subnet.rr.id
  route_table_id = aws_route_table.rr.id
}

resource "aws_security_group" "lab" {
  name        = "routerd-${var.run_id}-aws"
  description = "routerd SAM E2E AWS ${var.run_id}"
  vpc_id      = aws_vpc.lab.id

  ingress {
    description = "ssh"
    protocol    = "tcp"
    from_port   = 22
    to_port     = 22
    cidr_blocks = var.ssh_cidr_blocks
  }

  ingress {
    description = "wireguard"
    protocol    = "udp"
    from_port   = 51820
    to_port     = 51820
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "icmp"
    protocol    = "icmp"
    from_port   = -1
    to_port     = -1
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "same vpc"
    protocol    = "-1"
    from_port   = 0
    to_port     = 0
    cidr_blocks = [var.vpc_cidr]
  }

  egress {
    description = "all egress"
    protocol    = "-1"
    from_port   = 0
    to_port     = 0
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(local.common_tags, { Name = "routerd-${var.run_id}-aws" })
}

data "aws_iam_policy_document" "ec2_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "sam" {
  name               = "routerd-sam-e2e-${var.run_id}"
  assume_role_policy = data.aws_iam_policy_document.ec2_assume.json
  tags               = local.common_tags
}

data "aws_iam_policy_document" "sam_capture" {
  statement {
    effect = "Allow"
    actions = [
      "ec2:AssignPrivateIpAddresses",
      "ec2:UnassignPrivateIpAddresses",
      "ec2:DescribeInstances",
      "ec2:DescribeNetworkInterfaces",
      "ec2:DescribeRouteTables",
      "ec2:CreateRoute",
      "ec2:ReplaceRoute",
      "ec2:DeleteRoute",
      "ec2:ModifyNetworkInterfaceAttribute",
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "sam_capture" {
  name   = "routerd-sam-e2e-capture-${var.run_id}"
  role   = aws_iam_role.sam.id
  policy = data.aws_iam_policy_document.sam_capture.json
}

resource "aws_iam_instance_profile" "sam" {
  name = "routerd-sam-e2e-${var.run_id}"
  role = aws_iam_role.sam.name
  tags = local.common_tags
}

resource "aws_instance" "rr" {
  for_each                    = var.rr_nodes
  ami                         = var.ami_id
  instance_type               = var.instance_type
  key_name                    = var.key_name
  subnet_id                   = aws_subnet.rr.id
  private_ip                  = each.value.private_ip
  vpc_security_group_ids      = [aws_security_group.lab.id]
  iam_instance_profile        = aws_iam_instance_profile.sam.name
  associate_public_ip_address = true
  source_dest_check           = false
  user_data                   = local.router_cloud_init

  tags = merge(local.common_tags, {
    Name           = "routerd-${var.run_id}-${each.key}"
    "routerd-role" = "rr"
  })
}
