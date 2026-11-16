moved {
  from = module.tack_k3s.proxmox_download_file.cloud_image
  to   = module.tack_k3s_vm.proxmox_download_file.cloud_image
}

moved {
  from = module.tack_k3s.proxmox_virtual_environment_file.cloud_init
  to   = module.tack_k3s_vm.proxmox_virtual_environment_file.cloud_init
}

moved {
  from = module.tack_k3s.proxmox_virtual_environment_vm.server
  to   = module.tack_k3s_vm.proxmox_virtual_environment_vm.server
}

moved {
  from = module.tack_k3s.module.base
  to   = module.tack_k3s_base
}

moved {
  from = module.tack_k3s.pveguest_file.application_manifest
  to   = module.tack_application.pveguest_file.application_manifest
}

moved {
  from = module.tack_k3s.pveguest_file.ledger_ca
  to   = module.tack_application.pveguest_file.ledger_ca
}

moved {
  from = module.tack_k3s.pveguest_file.application_tmpfiles
  to   = module.tack_application.pveguest_file.application_tmpfiles
}

moved {
  from = module.tack_k3s.pveguest_file.application_files_service
  to   = module.tack_application.pveguest_file.application_files_service
}

moved {
  from = module.tack_k3s.pveguest_file.application_files_order
  to   = module.tack_application.pveguest_file.application_files_order
}

moved {
  from = module.tack_k3s.pveguest_file.application_secret
  to   = module.tack_application.pveguest_file.application_secret
}

moved {
  from = module.tack_k3s.pveguest_systemd_unit.application_files
  to   = module.tack_application.pveguest_systemd_unit.application_files
}
moved {
  from = module.tack_qa_services.pveguest_file.configuration["search/action_groups.yml"]
  to   = module.tack_qa_search.pveguest_file.configuration["search/action_groups.yml"]
}

moved {
  from = module.tack_qa_services.pveguest_file.configuration["search/allowlist.yml"]
  to   = module.tack_qa_search.pveguest_file.configuration["search/allowlist.yml"]
}

moved {
  from = module.tack_qa_services.pveguest_file.configuration["search/check-host.sh"]
  to   = module.tack_qa_search.pveguest_file.configuration["search/check-host.sh"]
}

moved {
  from = module.tack_qa_services.pveguest_file.configuration["search/compose.yaml"]
  to   = module.tack_qa_search.pveguest_file.configuration["search/compose.yaml"]
}

moved {
  from = module.tack_qa_services.pveguest_file.configuration["search/config.yml"]
  to   = module.tack_qa_search.pveguest_file.configuration["search/config.yml"]
}

moved {
  from = module.tack_qa_services.pveguest_file.configuration["search/nodes_dn.yml"]
  to   = module.tack_qa_search.pveguest_file.configuration["search/nodes_dn.yml"]
}

moved {
  from = module.tack_qa_services.pveguest_file.configuration["search/opensearch.yml"]
  to   = module.tack_qa_search.pveguest_file.configuration["search/opensearch.yml"]
}

moved {
  from = module.tack_qa_services.pveguest_file.configuration["search/roles.yml"]
  to   = module.tack_qa_search.pveguest_file.configuration["search/roles.yml"]
}

moved {
  from = module.tack_qa_services.pveguest_file.configuration["search/roles_mapping.yml"]
  to   = module.tack_qa_search.pveguest_file.configuration["search/roles_mapping.yml"]
}

moved {
  from = module.tack_qa_services.pveguest_file.configuration["search/security.sh"]
  to   = module.tack_qa_search.pveguest_file.configuration["search/security.sh"]
}

moved {
  from = module.tack_qa_services.pveguest_file.configuration["search/tenants.yml"]
  to   = module.tack_qa_search.pveguest_file.configuration["search/tenants.yml"]
}

moved {
  from = module.tack_qa_services.pveguest_file.secret["search/search_admin_certificate"]
  to   = module.tack_qa_search.pveguest_file.secret["search/search_admin_certificate"]
}

moved {
  from = module.tack_qa_services.pveguest_file.secret["search/search_admin_private_key"]
  to   = module.tack_qa_search.pveguest_file.secret["search/search_admin_private_key"]
}

moved {
  from = module.tack_qa_services.pveguest_file.secret["search/search_ca"]
  to   = module.tack_qa_search.pveguest_file.secret["search/search_ca"]
}

moved {
  from = module.tack_qa_services.pveguest_file.secret["search/search_certificate"]
  to   = module.tack_qa_search.pveguest_file.secret["search/search_certificate"]
}

moved {
  from = module.tack_qa_services.pveguest_file.secret["search/search_private_key"]
  to   = module.tack_qa_search.pveguest_file.secret["search/search_private_key"]
}

moved {
  from = module.tack_qa_services.pveguest_file.unit["search"]
  to   = module.tack_qa_search.pveguest_file.unit["search"]
}

moved {
  from = module.tack_qa_services.pveguest_systemd_unit.runtime["search"]
  to   = module.tack_qa_search.pveguest_systemd_unit.runtime["search"]
}

moved {
  from = module.tack_qa_services.pveguest_file.search_users
  to   = module.tack_qa_search.pveguest_file.search_users
}
