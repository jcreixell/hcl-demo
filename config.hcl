// version string attribute
version = "1.0.0"

// tags map attribute
tags = {
  "env" = "dev"
  "version" = "Hello, ${foo}!"
}

yo "sometype" "somename" {
    mama = "so fat"
}

pepe = "Hello ${config.yo.mama} ${config.yo.type} ${config.yo.identifier}!"

// Example of two user blocks. These will be handled as a slice of users in the `Configuration`
// struct.
user {
  username        = "Hello, ${bar}!"
  first_name      = "John"
  last_name       = "Flores"
  cloud_providers = ["AWS"]
  enabled         = false
}

user {
  username   = "yo.mama"
  first_name = "Foo"
  last_name  = "Bar"
  enabled    = true
}
