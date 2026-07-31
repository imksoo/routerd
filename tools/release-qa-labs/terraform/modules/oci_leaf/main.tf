locals {
  common_freeform_tags = {
    ManagedBy    = var.managed_by
    Owner        = var.managed_by
    Project      = "routerd-sam-e2e"
    Environment  = "validation"
    ExpiresAt    = var.expires_at
    Purpose      = var.purpose
    Commit       = var.commit
    RouterdRunId = var.run_id
  }
  node_freeform_tags = {
    for key, value in local.common_freeform_tags : key => value
    if key != "Owner"
  }

  nodes = {
    router = {
      name                   = var.router_name
      display_name           = "routerd-${var.run_id}-${var.router_name}"
      private_ip             = var.router_private_ip
      role                   = "leaf"
      skip_source_dest_check = true
      fault_domain           = "FAULT-DOMAIN-1"
      user_data              = base64encode(local.router_cloud_init)
    }
    client = {
      name                   = var.client_name
      display_name           = "routerd-${var.run_id}-${var.client_name}"
      private_ip             = var.client_private_ip
      role                   = "client"
      skip_source_dest_check = false
      fault_domain           = "FAULT-DOMAIN-2"
      user_data              = base64encode(local.client_cloud_init)
    }
  }

  extra_router_nodes = {
    for name, node in var.extra_leaf_nodes : name => {
      name                   = name
      display_name           = "routerd-${var.run_id}-${name}"
      private_ip             = node.router_private_ip
      role                   = "leaf"
      skip_source_dest_check = true
      fault_domain           = "FAULT-DOMAIN-1"
      user_data              = base64encode(local.router_cloud_init)
    }
  }

  extra_client_nodes = {
    for _, node in var.extra_leaf_nodes : node.client_name => {
      name                   = node.client_name
      display_name           = "routerd-${var.run_id}-${node.client_name}"
      private_ip             = node.client_private_ip
      role                   = "client"
      skip_source_dest_check = false
      fault_domain           = "FAULT-DOMAIN-2"
      user_data              = base64encode(local.client_cloud_init)
    }
  }

  all_nodes = merge(local.nodes, local.extra_router_nodes, local.extra_client_nodes)

  oci_firewall_open_commands = [
    "systemctl disable --now ufw firewalld netfilter-persistent 2>/dev/null || true",
    "for table in filter; do iptables -t $table -P INPUT ACCEPT 2>/dev/null || true; iptables -t $table -P FORWARD ACCEPT 2>/dev/null || true; iptables -t $table -P OUTPUT ACCEPT 2>/dev/null || true; iptables -t $table -F 2>/dev/null || true; iptables -t $table -X 2>/dev/null || true; done",
    "for table in filter; do ip6tables -t $table -P INPUT ACCEPT 2>/dev/null || true; ip6tables -t $table -P FORWARD ACCEPT 2>/dev/null || true; ip6tables -t $table -P OUTPUT ACCEPT 2>/dev/null || true; ip6tables -t $table -F 2>/dev/null || true; ip6tables -t $table -X 2>/dev/null || true; done",
  ]

  router_cloud_init = <<-CLOUD_INIT
    #cloud-config
    runcmd:
%{for command in local.oci_firewall_open_commands~}
      - [bash, -lc, ${jsonencode(command)}]
%{endfor~}
      - [bash, -lc, 'curl -fsSL https://raw.githubusercontent.com/oracle/oci-cli/master/scripts/install/install.sh -o /tmp/oci-cli-install.sh']
      - [bash, -lc, 'bash /tmp/oci-cli-install.sh --accept-all-defaults --install-dir /opt/oci-cli --exec-dir /usr/local/bin --script-dir /usr/local/bin']
      - [bash, -lc, 'oci --version']
      - [bash, -lc, 'rm -f /tmp/oci-cli-install.sh']
  CLOUD_INIT

  client_cloud_init = <<-CLOUD_INIT
    #cloud-config
    runcmd:
%{for command in local.oci_firewall_open_commands~}
      - [bash, -lc, ${jsonencode(command)}]
%{endfor~}
      - [bash, -lc, 'iptables -C INPUT -p tcp --dport 5201 -j ACCEPT 2>/dev/null || iptables -I INPUT 1 -p tcp --dport 5201 -j ACCEPT']
      - [bash, -lc, 'iptables -C INPUT -p udp --dport 5201 -j ACCEPT 2>/dev/null || iptables -I INPUT 1 -p udp --dport 5201 -j ACCEPT']
  CLOUD_INIT
}

resource "oci_core_vcn" "lab" {
  compartment_id = var.compartment_id
  cidr_block     = var.vcn_cidr
  display_name   = "vcn-routerd-${var.run_id}-oci"
  dns_label      = "rd${substr(md5(var.run_id), 0, 8)}"
  freeform_tags  = local.common_freeform_tags
}

resource "oci_core_internet_gateway" "lab" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.lab.id
  display_name   = "igw-routerd-${var.run_id}-oci"
  enabled        = true
  freeform_tags  = local.common_freeform_tags
}

resource "oci_core_route_table" "leaf" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.lab.id
  display_name   = "rt-routerd-${var.run_id}-oci-leaf"
  freeform_tags  = local.common_freeform_tags

  route_rules {
    destination       = "0.0.0.0/0"
    destination_type  = "CIDR_BLOCK"
    network_entity_id = oci_core_internet_gateway.lab.id
  }
}

resource "oci_core_security_list" "lab" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.lab.id
  display_name   = "sl-routerd-${var.run_id}-oci"
  freeform_tags  = local.common_freeform_tags

  dynamic "ingress_security_rules" {
    for_each = toset(var.ssh_cidr_blocks)
    content {
      protocol = "6"
      source   = ingress_security_rules.value
      tcp_options {
        min = 22
        max = 22
      }
    }
  }

  ingress_security_rules {
    protocol = "17"
    source   = "0.0.0.0/0"
    udp_options {
      min = 51820
      max = 51820
    }
  }

  ingress_security_rules {
    protocol = "1"
    source   = "0.0.0.0/0"
  }

  ingress_security_rules {
    protocol = "6"
    source   = "0.0.0.0/0"
    tcp_options {
      min = 5201
      max = 5201
    }
  }

  ingress_security_rules {
    protocol = "17"
    source   = "0.0.0.0/0"
    udp_options {
      min = 5201
      max = 5201
    }
  }

  ingress_security_rules {
    protocol = "all"
    source   = var.subnet_cidr
  }

  egress_security_rules {
    protocol    = "all"
    destination = "0.0.0.0/0"
  }
}

resource "oci_core_subnet" "leaf" {
  compartment_id             = var.compartment_id
  vcn_id                     = oci_core_vcn.lab.id
  cidr_block                 = var.subnet_cidr
  display_name               = "subnet-routerd-${var.run_id}-oci-leaf"
  dns_label                  = "leaf"
  route_table_id             = oci_core_route_table.leaf.id
  security_list_ids          = [oci_core_security_list.lab.id]
  prohibit_public_ip_on_vnic = false
  freeform_tags              = local.common_freeform_tags
}

resource "oci_core_instance" "node" {
  for_each             = local.all_nodes
  availability_domain  = var.availability_domain
  compartment_id       = var.compartment_id
  display_name         = each.value.display_name
  shape                = var.shape
  fault_domain         = each.value.fault_domain
  preserve_boot_volume = false
  freeform_tags = merge(local.node_freeform_tags, {
    RouterdNode        = each.value.name
    Role               = each.value.role
    cloudedge-mobility = each.value.role == "client" ? "true" : "false"
  })

  create_vnic_details {
    subnet_id              = oci_core_subnet.leaf.id
    display_name           = "${each.value.name}-primary-vnic"
    assign_public_ip       = true
    private_ip             = each.value.private_ip
    skip_source_dest_check = each.value.skip_source_dest_check
    freeform_tags = merge(local.node_freeform_tags, {
      RouterdNode        = each.value.name
      Role               = each.value.role
      cloudedge-mobility = each.value.role == "client" ? "true" : "false"
    })
  }

  metadata = merge({
    ssh_authorized_keys = "${var.ssh_public_key}\n"
    }, each.value.user_data == null ? {} : {
    user_data = each.value.user_data
  })

  source_details {
    source_type = "image"
    source_id   = var.image_id
  }

  dynamic "shape_config" {
    for_each = var.shape_ocpus == null ? [] : [1]
    content {
      ocpus         = var.shape_ocpus
      memory_in_gbs = var.shape_memory_in_gbs
    }
  }
}

data "oci_core_vnic_attachments" "node" {
  for_each       = oci_core_instance.node
  compartment_id = var.compartment_id
  instance_id    = each.value.id
}
