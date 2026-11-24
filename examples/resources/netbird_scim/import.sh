# Check the plan after the import: a planned change of `prefix` replaces the integration,
# which issues a new token. Leave `prefix` out if the plan shows one.
terraform import netbird_scim.example scim_integration_id

# For example

terraform import netbird_scim.example 7