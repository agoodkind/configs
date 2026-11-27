locals {
  credentials = var.vault_terrakube_credentials
  ldap_root   = "dc=terrakube,dc=internal"
  ldap_user   = "cn=operator,ou=users,${local.ldap_root}"
  ldap_admin  = "cn=admin,${local.ldap_root}"
  admin_group = "TERRAKUBE_ADMIN"
  dex_configuration = {
    issuer    = local.dex_issuer
    storage   = { type = "memory" }
    web       = { http = "[::]:5556" }
    telemetry = { http = "[::]:5558" }
    oauth2    = { responseTypes = ["code", "token", "id_token"], skipApprovalScreen = true }
    staticClients = [{
      id     = local.dex_client_id
      name   = "Terrakube"
      public = true
      redirectURIs = concat([local.dashboard_url], [
        for port in local.discovery["login.v1"].ports : "http://localhost:${port}/login"
      ])
    }]
    connectors = [{
      type = "ldap"
      name = "Local account"
      id   = "ldap"
      config = {
        host           = "terrakube-openldap-service:1389"
        insecureNoSSL  = true
        bindDN         = local.ldap_admin
        bindPW         = local.credentials.ldap_bind_password
        usernamePrompt = "Email address"
        userSearch = {
          baseDN    = "ou=users,${local.ldap_root}"
          filter    = "(objectClass=inetOrgPerson)"
          username  = "mail"
          idAttr    = "DN"
          emailAttr = "mail"
          nameAttr  = "cn"
        }
        groupSearch = {
          baseDN       = "ou=groups,${local.ldap_root}"
          filter       = "(objectClass=groupOfNames)"
          nameAttr     = "cn"
          userMatchers = [{ userAttr = "DN", groupAttr = "member" }]
        }
      }
    }]
  }
  signing_secret = base64encode(local.credentials.internal_secret)
  api_storage_credentials = {
    AwsStorageAccessKey = local.credentials.s3_access_key
    AwsStorageSecretKey = local.credentials.s3_secret_key
  }
  terrakube_secrets = {
    terrakube-api-secrets = merge(local.api_storage_credentials, {
      InternalSecret         = local.signing_secret
      PatSecret              = base64encode(local.credentials.pat_secret)
      DexClientId            = local.dex_client_id
      TerrakubeRedisPassword = local.credentials.valkey_password
      DatasourcePassword     = local.credentials.postgres_password
    })
    terrakube-executor-secrets = {
      InternalSecret              = local.signing_secret
      TerrakubeRedisPassword      = local.credentials.valkey_password
      AwsTerraformStateAccessKey  = local.credentials.s3_access_key
      AwsTerraformStateSecretKey  = local.credentials.s3_secret_key
      AwsTerraformOutputAccessKey = local.credentials.s3_access_key
      AwsTerraformOutputSecretKey = local.credentials.s3_secret_key
    }
    terrakube-registry-secrets = merge(local.api_storage_credentials, {
      InternalSecret = local.signing_secret
      PatSecret      = base64encode(local.credentials.pat_secret)
      AppClientId    = local.dex_client_id
    })
    terrakube-postgres = { password = local.credentials.postgres_password }
    terrakube-valkey   = { default = local.credentials.valkey_password }
    terrakube-dex      = { "config.yaml" = yamlencode(local.dex_configuration) }
    terrakube-ldap = {
      password = local.credentials.ldap_bind_password
      "local.ldif" = templatefile("${path.module}/files/local.ldif.tftpl", {
        root          = local.ldap_root
        user          = local.ldap_user
        group         = local.admin_group
        email         = base64encode(local.credentials.login_email)
        password_hash = base64encode(local.credentials.login_password_hash)
      })
    }
    terrakube-dashboard-tls = {
      "tls.crt" = local.credentials.dashboard_cert_pem
      "tls.key" = local.credentials.dashboard_key_pem
    }
  }
}

resource "pveguest_file" "secrets" {
  node = local.guest.node
  vmid = module.vm.vmid
  kind = "qemu"
  path = "${local.manifest_path}/terrakube-secrets.yaml"
  mode = "0600"
  content_wo = join("---\n", [for name, values in local.terrakube_secrets : yamlencode({
    apiVersion = "v1"
    kind       = "Secret"
    metadata   = { name = name, namespace = local.namespace }
    type       = name == "terrakube-dashboard-tls" ? "kubernetes.io/tls" : "Opaque"
    stringData = values
  })])
  content_wo_version = var.terrakube_credentials_version

  depends_on = [pveguest_file.namespace]
}
