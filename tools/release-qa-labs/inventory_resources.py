#!/usr/bin/env python3
"""Classify saved inventory responses; only corroborated instance tombstones are excluded."""
import argparse
import json
from pathlib import Path
import re
import sys


AWS_STATES = {"pending", "running", "shutting-down", "terminated", "stopping", "stopped"}
OCI_STATES = {"MOVING", "PROVISIONING", "RUNNING", "STARTING", "STOPPING", "STOPPED",
              "CREATING_IMAGE", "TERMINATING", "TERMINATED"}
INSTANCE_ID = re.compile(r"i-(?:[0-9a-f]{8}|[0-9a-f]{17})")
INSTANCE_ARN = re.compile(
    r"arn:aws(?:-[a-z-]+)?:ec2:([a-z0-9-]+):([0-9]{12}):instance/(i-(?:[0-9a-f]{8}|[0-9a-f]{17}))"
)


class InventoryError(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise InventoryError(message)


def object_value(value, label):
    require(isinstance(value, dict), label + " must be an object")
    return value


def array_value(value, label):
    require(isinstance(value, list), label + " must be an array")
    return value


def text_value(value, label):
    require(isinstance(value, str) and bool(value.strip()), label + " must be a nonempty string")
    return value


def load_json(path):
    def unique_pairs(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, "duplicate JSON key")
            result[key] = value
        return result

    def reject_constant(_value):
        raise InventoryError("invalid JSON constant")

    try:
        return json.loads(Path(path).read_text(encoding="utf-8"), object_pairs_hook=unique_pairs,
                          parse_constant=reject_constant)
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise InventoryError("cannot read provider response JSON") from error


def complete_response(data):
    object_value(data, "provider response")
    for key in ("NextToken", "PaginationToken", "opc-next-page", "next-page"):
        require(key not in data or data[key] is None or data[key] == "",
                "provider response is partial or has invalid pagination metadata")


def aws_tags(tags, run_id):
    values = {}
    for tag in array_value(tags, "AWS tags"):
        object_value(tag, "AWS tag")
        key = text_value(tag.get("Key"), "AWS tag key")
        require(key not in values and isinstance(tag.get("Value"), str), "invalid or duplicate AWS tag")
        values[key] = tag["Value"]
    require(values.get("routerd-run-id") == run_id, "AWS run tag does not match")


def aws_tagged(data, run_id, region):
    complete_response(data)
    resources = {}
    instances = {}
    for row in array_value(data.get("ResourceTagMappingList"), "AWS tagged resources"):
        object_value(row, "AWS tagged resource")
        arn = text_value(row.get("ResourceARN"), "AWS resource ARN")
        require(arn not in resources, "duplicate AWS resource ARN")
        aws_tags(row.get("Tags"), run_id)
        resources[arn] = row
        match = INSTANCE_ARN.fullmatch(arn)
        if match:
            found_region, account, instance = match.groups()
            require(found_region == region, "AWS instance ARN region does not match")
            require(instance not in instances, "duplicate AWS instance identity")
            instances[instance] = account
        else:
            require(not (":ec2:" in arn and ":instance/" in arn), "malformed AWS instance ARN")
    return resources, instances


def aws_ids(data, run_id, region):
    return sorted(aws_tagged(data, run_id, region)[1])


def aws_instances(data, run_id):
    complete_response(data)
    instances = {}
    for reservation in array_value(data.get("Reservations"), "AWS reservations"):
        object_value(reservation, "AWS reservation")
        owner = text_value(reservation.get("OwnerId"), "AWS reservation owner")
        require(re.fullmatch(r"[0-9]{12}", owner), "invalid AWS reservation owner")
        for row in array_value(reservation.get("Instances"), "AWS instances"):
            object_value(row, "AWS instance")
            instance = text_value(row.get("InstanceId"), "AWS instance ID")
            require(INSTANCE_ID.fullmatch(instance) and instance not in instances, "invalid or duplicate AWS instance ID")
            aws_tags(row.get("Tags"), run_id)
            state = object_value(row.get("State"), "AWS instance state").get("Name")
            require(isinstance(state, str) and state in AWS_STATES, "unknown AWS instance state")
            instances[instance] = (owner, state)
    return instances


def counts(raw_count, terminated, active_count, live_count):
    return {"rawTaggedCount": raw_count, "confirmedTerminatedCount": terminated,
            "activeInstanceCount": active_count, "count": live_count}


def aws_counts(tagged, lookup, active, run_id, region):
    resources, expected = aws_tagged(tagged, run_id, region)
    resolved = aws_instances(lookup, run_id)
    active_instances = aws_instances(active, run_id)
    require(set(expected) == set(resolved), "AWS exact-ID lookup is missing or has extra instances")
    for instance, account in expected.items():
        require(resolved[instance][0] == account, "AWS ARN and lookup account differ")
    for instance, identity in active_instances.items():
        require(identity[1] != "terminated", "AWS active query unexpectedly returned terminated state")
        require(instance not in resolved or resolved[instance] == identity, "AWS instance observations disagree")
    terminated = sum(state == "terminated" for _, state in resolved.values())
    # A delayed tagging index must not hide a live instance seen by EC2.
    additional_active = len(set(active_instances) - set(resolved))
    return counts(len(resources), terminated, len(active_instances), len(resources) - terminated + additional_active)


def oci_page(data):
    object_value(data, "OCI search page")
    body = object_value(data.get("data"), "OCI search data")
    array_value(body.get("items"), "OCI search items")
    require(not any(key in data for key in ("next-page", "NextToken", "PaginationToken")),
            "ambiguous OCI search pagination")
    token = data.get("opc-next-page")
    require(token is None or (isinstance(token, str) and len(token) <= 4096 and not any(
        character in token for character in ("\n", "\r", "\x00"))), "invalid OCI search page token")


def oci_counts(search, compute, run_id, compartment):
    complete_response(compute)
    instances = {}
    for row in array_value(compute.get("data"), "OCI compute instances"):
        object_value(row, "OCI compute instance")
        instance = text_value(row.get("id"), "OCI instance ID")
        require(instance not in instances, "duplicate OCI compute instance")
        tags = object_value(row.get("freeform-tags"), "OCI instance tags")
        instances[instance] = row
        if tags.get("RouterdRunId") == run_id:
            require(row.get("compartment-id") == compartment, "OCI compute compartment differs")
            require(isinstance(row.get("lifecycle-state"), str) and row["lifecycle-state"] in OCI_STATES,
                    "unknown OCI compute lifecycle state")
    active = {instance for instance, row in instances.items()
              if row["freeform-tags"].get("RouterdRunId") == run_id and row["lifecycle-state"] != "TERMINATED"}
    complete_response(search)
    oci_page(search)
    pagination = object_value(search.get("pagination"), "OCI search pagination")
    require(pagination.get("status") == "complete" and type(pagination.get("pages")) is int
            and pagination["pages"] > 0, "OCI search pagination is not complete")
    identifiers = set()
    searched_instances = set()
    terminated = 0
    for row in search["data"]["items"]:
        object_value(row, "OCI search resource")
        identifier = text_value(row.get("identifier"), "OCI search identifier")
        require(identifier not in identifiers, "duplicate OCI search identifier")
        identifiers.add(identifier)
        tags = object_value(row.get("freeform-tags"), "OCI search resource tags")
        require(tags.get("RouterdRunId") == run_id, "OCI search run tag differs")
        require(row.get("compartment-id") == compartment, "OCI search compartment differs")
        kind = text_value(row.get("resource-type"), "OCI search resource type")
        if kind != "Instance":
            continue  # No lifecycle label can exempt a non-instance resource.
        require(identifier.startswith("ocid1.instance."), "invalid OCI instance identifier")
        state = row.get("lifecycle-state")
        require(isinstance(state, str) and state in OCI_STATES, "unknown OCI search lifecycle state")
        observed = instances.get(identifier)
        require(observed is not None, "OCI search instance is missing from compute inventory")
        require(observed["freeform-tags"].get("RouterdRunId") == run_id
                and observed.get("compartment-id") == compartment
                and observed.get("lifecycle-state") == state, "OCI search and compute identity/state differ")
        searched_instances.add(identifier)
        terminated += state == "TERMINATED"
    return counts(len(identifiers), terminated, len(active),
                  len(identifiers) - terminated + len(active - searched_instances))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)
    for name in ("aws-ids", "aws-counts", "oci-counts"):
        child = subparsers.add_parser(name)
        child.add_argument("--run-id", required=True)
        child.add_argument("--tagged", type=Path, required=True)
        if name.startswith("aws"):
            child.add_argument("--region", required=True)
        if name == "aws-counts":
            child.add_argument("--lookup", type=Path, required=True)
            child.add_argument("--active", type=Path, required=True)
        if name == "oci-counts":
            child.add_argument("--compute", type=Path, required=True)
            child.add_argument("--compartment", required=True)
    subparsers.add_parser("oci-page").add_argument("path", type=Path)
    args = parser.parse_args()
    if args.command == "oci-page":
        oci_page(load_json(args.path))
        return
    tagged = load_json(args.tagged)
    if args.command == "aws-ids":
        result = aws_ids(tagged, args.run_id, args.region)
    elif args.command == "aws-counts":
        result = aws_counts(tagged, load_json(args.lookup), load_json(args.active), args.run_id, args.region)
    else:
        result = oci_counts(tagged, load_json(args.compute), args.run_id, args.compartment)
    print(json.dumps(result))


if __name__ == "__main__":
    try:
        main()
    except InventoryError as error:
        print("inventory resources: " + str(error), file=sys.stderr)
        sys.exit(2)
