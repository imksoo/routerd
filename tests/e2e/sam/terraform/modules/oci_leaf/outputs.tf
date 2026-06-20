output "vcn_id" { value = oci_core_vcn.lab.id }
output "subnet_id" { value = oci_core_subnet.leaf.id }
output "route_table_id" { value = oci_core_route_table.leaf.id }

output "router" {
  value = {
    name         = var.router_name
    role         = "leaf"
    site         = "oci"
    ssh_user     = "ubuntu"
    instance_id  = oci_core_instance.node["router"].id
    interface_id = data.oci_core_vnic_attachments.node["router"].vnic_attachments[0].vnic_id
    private_ip   = local.nodes.router.private_ip
    public_ip    = oci_core_instance.node["router"].public_ip
  }
}

output "client" {
  value = {
    name         = var.client_name
    role         = "client"
    site         = "oci"
    ssh_user     = "ubuntu"
    instance_id  = oci_core_instance.node["client"].id
    interface_id = data.oci_core_vnic_attachments.node["client"].vnic_attachments[0].vnic_id
    private_ip   = local.nodes.client.private_ip
    public_ip    = oci_core_instance.node["client"].public_ip
  }
}

output "routers" {
  value = {
    for key, node in local.all_nodes : node.name => {
      name         = node.name
      role         = "leaf"
      site         = "oci"
      ssh_user     = "ubuntu"
      instance_id  = oci_core_instance.node[key].id
      interface_id = data.oci_core_vnic_attachments.node[key].vnic_attachments[0].vnic_id
      private_ip   = node.private_ip
      public_ip    = oci_core_instance.node[key].public_ip
    } if node.role == "leaf"
  }
}

output "clients" {
  value = {
    for key, node in local.all_nodes : node.name => {
      name         = node.name
      role         = "client"
      site         = "oci"
      ssh_user     = "ubuntu"
      instance_id  = oci_core_instance.node[key].id
      interface_id = data.oci_core_vnic_attachments.node[key].vnic_attachments[0].vnic_id
      private_ip   = node.private_ip
      public_ip    = oci_core_instance.node[key].public_ip
    } if node.role == "client"
  }
}
