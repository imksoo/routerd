# CloudEdge Mobility Demo IAM Templates

These templates document the provider permissions used by the CloudEdge Mobility
demo after the autonomous BGP mobility path passed. They are examples for
least-privilege review; this directory does not apply cloud policy and the demo
scripts do not provision IAM.

The routerd provider executor and provider inventory plugin authenticate with
cloud-native identity on the router instance:

- AWS: EC2 instance profile.
- Azure: managed identity assigned to the VM.
- OCI: instance principal through a dynamic group.

routerd core does not hold cloud credentials. It emits fenced provider action
plans and invokes provider plugins; the cloud CLI or provider runtime resolves
the instance identity.

## What Is In Scope

The policies cover only routerd's provider mutation and inventory path for the
mobility demo:

| Cloud | API surface |
| --- | --- |
| AWS | `ec2:AssignPrivateIpAddresses`, `ec2:UnassignPrivateIpAddresses`, `ec2:DescribeNetworkInterfaces`, `ec2:ModifyNetworkInterfaceAttribute` |
| Azure | NIC read/write, child `ipConfigurations` read/write/delete/join, and linked `subnets` / `networkSecurityGroups` / `publicIPAddresses` read + join |
| OCI | `CreatePrivateIp`, `DeletePrivateIp`, `ListPrivateIps`, `GetVnic`, `UpdateVnic`, and subnet read/use |

Harness permissions are intentionally not included. Starting/stopping VMs,
creating NICs/VNICs, changing NSGs/security lists, uploading files, and other lab
orchestration steps belong to the operator harness, not to routerd running on the
router instance.

## Why Forwarding Permission Is Required

Cloud router instances must forward traffic for captured remote `/32` addresses.
Provider fabrics commonly block forwarding unless an instance-level NIC/VNIC
flag is changed:

- AWS: `SourceDestCheck=false` on the ENI.
- Azure: `enableIPForwarding=true` on the NIC.
- OCI: `skipSourceDestCheck=true` on the VNIC.

The provider executor records prior state in the action journal before changing
these flags, so rollback can restore only what routerd changed.

## AWS

Template: `aws-policy.json`.

Attach the policy to the instance profile used by each AWS router. Replace:

- `<aws-region>` with the demo region.
- `<aws-account-id>` with the AWS account ID.
- `<eni-id>` with the router ENI ID for that router instance.

The mutating EC2 actions are scoped to the target ENI ARN. EC2 describe APIs are
not scoped to an individual ENI resource in the same way, so the template keeps
`Resource: "*"` for `DescribeNetworkInterfaces` and restricts it by
`aws:RequestedRegion`.

If a router has both active and standby ENIs, prefer one policy document per
instance profile with only that instance's own ENI ARN. Do not grant a router
permission to mutate another provider's NIC or the harness control plane.

## Azure

Template: `azure-custom-role.json`.

Create a custom role from the JSON and assign it to the VM's managed identity.
Replace:

- `<subscription-id>` with the subscription ID.
- `<resource-group>` with the smallest resource group containing the target NIC.

This replaces broad `Network Contributor` for routerd. The role permits NIC read
and write plus child `ipConfigurations` read/write/delete. Those are the
operations the executor uses to enable forwarding and create/delete captured
secondary IP configurations.

Azure ARM also authorizes linked resources when an `ipConfiguration` is written.
If the ipConfig references a subnet, network security group, or public IP,
the managed identity needs `join/action` and read permission on those linked
resources:

- `Microsoft.Network/virtualNetworks/subnets/read`
- `Microsoft.Network/virtualNetworks/subnets/join/action`
- `Microsoft.Network/networkSecurityGroups/read`
- `Microsoft.Network/networkSecurityGroups/join/action`
- `Microsoft.Network/publicIPAddresses/read`
- `Microsoft.Network/publicIPAddresses/join/action`
- `Microsoft.Network/networkInterfaces/ipConfigurations/join/action`

The template includes these actions. In some Azure tenants, strict custom-role
authorization still fails linked-resource checks for fixed subnet/NSG/PIP
resources. In that case, keep the custom role for the NIC operations and add a
fallback assignment of the built-in **Network Contributor** role scoped only to
the specific linked subnet, NSG, and public IP resources, or to the smallest
resource group containing only those linked resources. That is still much
narrower than subscription Admin or broad subscription-wide Network Contributor.

For tighter deployments, scope `AssignableScopes` to a dedicated resource group
or to the specific NIC resource ID if your Azure role assignment workflow allows
that scope cleanly.

## OCI

Template: `oci-policy.txt`.

Create a dynamic group that matches only the router instances, then add the
policy statements in the compartment containing the router VNIC and private IPs.
Replace:

- `<dynamic-group-name>` with the dynamic group name.
- `<compartment-name>` with the target compartment name.

The `private-ips` permission covers create/delete/list of captured private IP
objects. The `vnics` permission covers reading the VNIC and updating
`skipSourceDestCheck`. The `subnets` permission covers reading/using the subnet
referenced by VNIC private-IP placement.

Keep the dynamic group narrow. It should match the router instances only, not the
demo clients and not the operator harness.

## Migration From Admin / Broad Roles

1. Keep the current broad role in place.
2. Attach the least-privilege template to the router identity.
3. Run read-only validation with the broad role still present:
   - provider inventory scan observes only mobility candidate addresses;
   - routerd status shows the expected self NIC/VNIC and subnet;
   - pending provider action plans target only the router NIC/VNIC.
4. Remove the broad role.
5. Restart routerd or wait for the next reconcile.
6. Verify:
   - provider inventory still succeeds;
   - forwarding ensure action can reconcile drift;
   - `assign-secondary-ip` / `unassign-secondary-ip` succeeds for the mobility
     `/32` range;
   - no harness-only operation is attempted by routerd.

For this repository task, stop at templates and documentation. Applying the
policies and rerunning the live demo is a separate cost-bearing lab step.

## Scoped Retest Evidence

The least-privilege surface above was corrected after a scoped provider-action
retest captured in `evidence/20260604T051859Z-leastpriv-a2434c96`:

- AWS succeeded with the ENI-scoped four-action policy in `aws-policy.json`.
- OCI succeeded with compartment-scoped `private-ips`, `vnics`, and `subnets`
  permissions in `oci-policy.txt`.
- Azure required the linked-resource join/read surface above. Where a strict
  custom role alone does not satisfy ARM linked authorization, use the
  resource-scoped Network Contributor fallback for the fixed subnet, NSG, and
  public IP resources.
