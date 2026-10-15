"""Validate the OpenAPI document, local references, and all embedded examples."""

from copy import deepcopy
from pathlib import Path

import yaml
from openapi_schema_validator import OAS30Validator
from openapi_spec_validator import validate


class UniqueKeyLoader(yaml.SafeLoader):
    """Reject duplicate YAML keys instead of silently replacing documentation."""


def unique_mapping(loader, node):
    result = {}
    for key_node, value_node in node.value:
        key = loader.construct_object(key_node)
        if key in result:
            raise ValueError(f"Duplicate YAML key: {key}")
        result[key] = loader.construct_object(value_node)
    return result


UniqueKeyLoader.add_constructor(
    yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, unique_mapping
)
path = Path(__file__).resolve().parents[1] / "docs" / "openapi.yaml"
spec = yaml.load(path.read_text(encoding="utf-8"), Loader=UniqueKeyLoader)
validate(spec)


def resolve(value):
    if isinstance(value, list):
        return [resolve(item) for item in value]
    if not isinstance(value, dict):
        return value
    if "$ref" in value:
        reference = value["$ref"]
        if not reference.startswith("#/"):
            raise ValueError(f"Only local references are supported: {reference}")
        target = spec
        for part in reference[2:].split("/"):
            target = target[part.replace("~1", "/").replace("~0", "~")]
        return resolve(deepcopy(target))
    return {key: resolve(item) for key, item in value.items()}


example_count = 0


def check_examples(value):
    global example_count
    if isinstance(value, list):
        for item in value:
            check_examples(item)
    elif isinstance(value, dict):
        schema = value.get("schema")
        if schema:
            validator = OAS30Validator(resolve(schema))
            if "example" in value:
                validator.validate(value["example"])
                example_count += 1
            for example in value.get("examples", {}).values():
                validator.validate(example["value"])
                example_count += 1
        for item in value.values():
            check_examples(item)


check_examples(spec)
operations = sum(
    method in {"get", "post", "patch", "delete"}
    for item in spec["paths"].values()
    for method in item
)
print(f"OpenAPI valid: {len(spec['paths'])} paths, {operations} operations, "
      f"{example_count} examples")
