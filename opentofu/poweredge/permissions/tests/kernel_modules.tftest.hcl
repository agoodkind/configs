run "poweredge_kernel_module_grant" {
  command = plan

  assert {
    condition = (
      length(proxmox_acl.kernel_modules) == 0 ?
      anytrue([
        for acl in data.proxmox_virtual_environment_user.automation.acl :
        acl.path == "/" && acl.role_id == "ScopedKernelModules" && acl.propagate
        ]) : (
        proxmox_acl.kernel_modules[0].path == "/nodes/poweredge" &&
        proxmox_acl.kernel_modules[0].user_id == "ansible@pam" &&
        proxmox_acl.kernel_modules[0].role_id == "ScopedKernelModules" &&
        proxmox_acl.kernel_modules[0].propagate
      )
    )
    error_message = "The automation account needs the kernel module role on PowerEdge, either directly or through an inherited grant."
  }
}
