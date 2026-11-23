# retrieve by name
data "netbird_network" "by_name" {
  name = "TF Test"
}

# retrieve by ID
data "netbird_network" "by_id" {
  id = "cvit5sjl0ubs73aslrkg"
}
