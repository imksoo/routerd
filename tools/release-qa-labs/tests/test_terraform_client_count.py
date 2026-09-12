"""Provider-free checks: execute the real topology expressions with tofu console.

No provider initialization, credentials, network, or host mutation is used.
"""
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


TF = Path(__file__).resolve().parents[1] / "terraform"
SITES = ("aws", "azure", "oci", "pve")


def block(text, prefix):
    start = text.index(prefix)
    opening = text.index("{", start)
    # Topology assignments start with an empty map on the false branch.
    if text[opening:opening + 5] == "{} : ":
        opening = text.index("{", opening + 5)
    depth = 1
    pos = opening + 1
    while depth:
        depth += (text[pos] == "{") - (text[pos] == "}")
        pos += 1
    return text[start:pos]


class TerraformClientCountTests(unittest.TestCase):
    def test_client_count_defaults_to_one_and_is_bounded(self):
        variables = (TF / "envs/default/variables.tf").read_text()
        count = block(variables, 'variable "clients_per_site"')
        self.assertRegex(count, r"default\s*=\s*1\b")
        self.assertIn("contains([1, 2], var.clients_per_site)", count)

    @unittest.skipUnless(shutil.which("tofu"), "tofu console is required")
    def test_real_topology_expressions_keep_routers_and_clients_independent(self):
        main = (TF / "envs/default/main.tf").read_text()
        expressions = [block(main, f"  {site}_extra_{role}_nodes =")
                       for site in SITES for role in ("leaf", "client")]
        # The expressions contain only these inputs; the provider-backed root
        # module is intentionally never initialized or planned by this test.
        variables = "\n".join(f'variable "{name}" {{ type = {"string" if name == "topology_scale" else "number"} }}' for name in (
            "topology_scale", "clients_per_site", "pve_leaf_b_router_vm_id",
            "pve_leaf_b_client_vm_id"))
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp)
            (path / "main.tf").write_text(variables + "\nlocals {\n" + "\n".join(expressions) + "\n}\n")
            for scale in ("single", "full"):
                for clients in (1, 2):
                    with self.subTest(scale=scale, clients=clients):
                        query = "jsonencode({" + ",".join(
                            f'{site}={{routers=1+length(local.{site}_extra_leaf_nodes),clients=1+length(local.{site}_extra_client_nodes)}}'
                            for site in SITES) + "})\n"
                        result = subprocess.run([
                            "tofu", f"-chdir={temp}", "console", "-no-color",
                            f"-var=topology_scale={scale}", f"-var=clients_per_site={clients}",
                            "-var=pve_leaf_b_router_vm_id=9003", "-var=pve_leaf_b_client_vm_id=9004",
                        ], input=query, text=True, capture_output=True, timeout=20)
                        self.assertEqual(result.returncode, 0, result.stderr)
                        actual = json.loads(json.loads(result.stdout))
                        self.assertEqual(actual, {site: {"routers": 2 if scale == "full" else 1,
                                                         "clients": clients} for site in SITES})

    def test_each_module_accepts_independent_extra_clients(self):
        main = (TF / "envs/default/main.tf").read_text()
        for site in SITES:
            with self.subTest(site=site):
                call = block(main, f'module "{site}_leaf"')
                self.assertRegex(call, rf"extra_client_nodes\s*=\s*local\.{site}_extra_client_nodes")
                variables = (TF / f"modules/{site}_leaf/variables.tf").read_text()
                self.assertIn('variable "extra_client_nodes"', variables)
                leaves = block(variables, 'variable "extra_leaf_nodes"')
                self.assertNotIn("client_", leaves)
                module = (TF / f"modules/{site}_leaf/main.tf").read_text()
                if site == "aws":
                    clients = block(module, 'resource "aws_instance" "extra_client"')
                else:
                    clients = block(module, "  extra_client_nodes =")
                self.assertIn("var.extra_client_nodes", clients)
                self.assertNotIn("var.extra_leaf_nodes", clients)

    def test_full_pve_does_not_require_unprovisioned_client_b(self):
        main = (TF / "envs/default/main.tf").read_text()
        self.assertIn("var.clients_per_site == 1 || var.pve_leaf_b_client_vm_id != null", main)
        self.assertIn("var.clients_per_site == 2 ? [var.pve_leaf_b_client_vm_id] : []", main)

    def test_fabric_exposes_client_cardinality(self):
        outputs = (TF / "envs/default/outputs.tf").read_text()
        self.assertRegex(outputs, r"clients_per_site\s*=\s*var.clients_per_site")

    def test_reviewed_small_instance_defaults(self):
        main = (TF / "envs/default/main.tf").read_text()
        self.assertRegex(block(main, 'module "aws_leaf"'), r'instance_type\s*=\s*"t3.small"')
        variables = (TF / "envs/default/variables.tf").read_text()
        self.assertIn('"VM.Standard.E4.Flex"', block(variables, 'variable "oci_shape"'))
        for name in ("oci_shape_ocpus", "oci_shape_memory_in_gbs"):
            self.assertRegex(block(variables, f'variable "{name}"'), r"default\s*=\s*1\b")
        for module in ("pve_leaf", "pve_rr"):
            variables = (TF / f"modules/{module}/variables.tf").read_text()
            self.assertRegex(block(variables, 'variable "cpu_cores"'), r"default\s*=\s*1\b")
            self.assertRegex(block(variables, 'variable "memory_mb"'), r"default\s*=\s*1024\b")


if __name__ == "__main__":
    unittest.main()
