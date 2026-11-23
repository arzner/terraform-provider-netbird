# The account settings of the account the token belongs to
terraform import netbird_account_settings.example current

# Or by account ID (GET /api/accounts)
terraform import netbird_account_settings.example account_id
