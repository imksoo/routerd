locals {
  common_tags = {
    ManagedBy         = "codex"
    Owner             = "codex"
    Project           = "routerd-cloudedge-sam"
    Environment       = "validation"
    ExpiresAt         = var.expires_at
    "routerd-purpose" = var.purpose
    "routerd-commit"  = var.commit
    "routerd-run-id"  = var.run_id
  }

  router_cloud_init = <<-CLOUD_INIT
    #cloud-config
    package_update: true
    packages:
      - curl
      - unzip
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

  tags = merge(local.common_tags, {
    Name = "routerd-${var.run_id}"
  })
}

resource "aws_internet_gateway" "lab" {
  vpc_id = aws_vpc.lab.id

  tags = merge(local.common_tags, {
    Name = "routerd-${var.run_id}"
  })
}

resource "aws_subnet" "lab" {
  vpc_id                  = aws_vpc.lab.id
  cidr_block              = var.subnet_cidr
  map_public_ip_on_launch = true

  tags = merge(local.common_tags, {
    Name = "routerd-${var.run_id}"
  })
}

resource "aws_route_table" "lab" {
  vpc_id = aws_vpc.lab.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.lab.id
  }

  tags = merge(local.common_tags, {
    Name = "routerd-${var.run_id}"
  })
}

resource "aws_route_table_association" "lab" {
  subnet_id      = aws_subnet.lab.id
  route_table_id = aws_route_table.lab.id
}

resource "aws_security_group" "lab" {
  name        = "routerd-${var.run_id}"
  description = "routerd CloudEdge SAM ${var.run_id}"
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
    description = "same subnet"
    protocol    = "-1"
    from_port   = 0
    to_port     = 0
    cidr_blocks = [var.subnet_cidr]
  }

  egress {
    description = "all egress"
    protocol    = "-1"
    from_port   = 0
    to_port     = 0
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(local.common_tags, {
    Name = "routerd-${var.run_id}"
  })
}

locals {
  manage_iam           = var.iam_instance_profile == ""
  instance_profile_name = local.manage_iam ? aws_iam_instance_profile.sam[0].name : var.iam_instance_profile

  secondary_ip_actions = [
    "ec2:AssignPrivateIpAddresses",
    "ec2:UnassignPrivateIpAddresses",
    "ec2:DescribeInstances",
    "ec2:DescribeNetworkInterfaces",
  ]

  route_table_actions = [
    "ec2:DescribeRouteTables",
    "ec2:CreateRoute",
    "ec2:ReplaceRoute",
    "ec2:DeleteRoute",
  ]

  capture_actions = concat(
    contains(var.capture_strategies, "secondary-ip") ? local.secondary_ip_actions : [],
    contains(var.capture_strategies, "route-table") ? local.route_table_actions : [],
  )
}

data "aws_iam_policy_document" "ec2_assume" {
  count = local.manage_iam ? 1 : 0

  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "sam" {
  count = local.manage_iam ? 1 : 0

  name               = "routerd-sam-${var.run_id}"
  assume_role_policy = data.aws_iam_policy_document.ec2_assume[0].json
  tags               = local.common_tags
}

data "aws_iam_policy_document" "sam_capture" {
  count = local.manage_iam ? 1 : 0

  statement {
    effect    = "Allow"
    actions   = local.capture_actions
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "sam_capture" {
  count = local.manage_iam ? 1 : 0

  name   = "routerd-sam-capture-${var.run_id}"
  role   = aws_iam_role.sam[0].id
  policy = data.aws_iam_policy_document.sam_capture[0].json
}

resource "aws_iam_instance_profile" "sam" {
  count = local.manage_iam ? 1 : 0

  name = "routerd-sam-${var.run_id}"
  role = aws_iam_role.sam[0].name
  tags = local.common_tags
}

resource "aws_instance" "router_a" {
  ami                         = var.ami_id
  instance_type               = var.instance_type
  key_name                    = var.key_name
  subnet_id                   = aws_subnet.lab.id
  private_ip                  = var.router_a_private_ip
  vpc_security_group_ids      = [aws_security_group.lab.id]
  iam_instance_profile        = local.instance_profile_name
  associate_public_ip_address = true
  source_dest_check           = false
  user_data                   = local.router_cloud_init

  tags = merge(local.common_tags, {
    Name           = "routerd-${var.run_id}-a"
    "routerd-role" = "router-a"
  })

  volume_tags = merge(local.common_tags, {
    Name           = "routerd-${var.run_id}-a"
    "routerd-role" = "router-a"
  })
}

resource "aws_instance" "router_b" {
  ami                         = var.ami_id
  instance_type               = var.instance_type
  key_name                    = var.key_name
  subnet_id                   = aws_subnet.lab.id
  private_ip                  = var.router_b_private_ip
  vpc_security_group_ids      = [aws_security_group.lab.id]
  iam_instance_profile        = local.instance_profile_name
  associate_public_ip_address = true
  source_dest_check           = false
  user_data                   = local.router_cloud_init

  tags = merge(local.common_tags, {
    Name           = "routerd-${var.run_id}-b"
    "routerd-role" = "router-b"
  })

  volume_tags = merge(local.common_tags, {
    Name           = "routerd-${var.run_id}-b"
    "routerd-role" = "router-b"
  })
}

resource "aws_instance" "client" {
  ami                         = var.ami_id
  instance_type               = var.instance_type
  key_name                    = var.key_name
  subnet_id                   = aws_subnet.lab.id
  private_ip                  = var.client_private_ip
  vpc_security_group_ids      = [aws_security_group.lab.id]
  associate_public_ip_address = true

  tags = merge(local.common_tags, {
    Name           = "routerd-${var.run_id}-client"
    "routerd-role" = "client"
  })

  volume_tags = merge(local.common_tags, {
    Name           = "routerd-${var.run_id}-client"
    "routerd-role" = "client"
  })
}

locals {
  node_instance_ids = {
    router_a = aws_instance.router_a.id
    router_b = aws_instance.router_b.id
    client   = aws_instance.client.id
  }

  node_eip_tags = {
    router_a = merge(local.common_tags, {
      Name           = "routerd-${var.run_id}-a-eip"
      "routerd-role" = "router-a"
    })
    router_b = merge(local.common_tags, {
      Name           = "routerd-${var.run_id}-b-eip"
      "routerd-role" = "router-b"
    })
    client = merge(local.common_tags, {
      Name           = "routerd-${var.run_id}-client-eip"
      "routerd-role" = "client"
    })
  }

  primary_eni_tags = {
    router_a = merge(local.common_tags, {
      Name           = "routerd-${var.run_id}-a-primary-eni"
      "routerd-role" = "router-a"
    })
    router_b = merge(local.common_tags, {
      Name           = "routerd-${var.run_id}-b-primary-eni"
      "routerd-role" = "router-b"
    })
    client = merge(local.common_tags, {
      Name           = "routerd-${var.run_id}-client-primary-eni"
      "routerd-role" = "client"
    })
  }

  primary_eni_ids = {
    router_a = aws_instance.router_a.primary_network_interface_id
    router_b = aws_instance.router_b.primary_network_interface_id
    client   = aws_instance.client.primary_network_interface_id
  }
}

resource "aws_eip" "node" {
  for_each = local.node_instance_ids

  domain = "vpc"
  tags   = local.node_eip_tags[each.key]
}

resource "aws_eip_association" "node" {
  for_each = local.node_instance_ids

  allocation_id = aws_eip.node[each.key].id
  instance_id   = each.value
}

resource "aws_ec2_tag" "primary_eni" {
  for_each = merge([
    for role, tags in local.primary_eni_tags : {
      for key, value in tags : "${role}:${key}" => {
        resource_id = local.primary_eni_ids[role]
        key         = key
        value       = value
      }
    }
  ]...)

  resource_id = each.value.resource_id
  key         = each.value.key
  value       = each.value.value
}
