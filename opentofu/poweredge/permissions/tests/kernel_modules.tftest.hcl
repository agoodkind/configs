run "poweredge_kernel_module_grant" {
  command = plan

  assert {
    condition = (
      proxmox_acl.kernel_modules.path == "/nodes/poweredge" &&
      proxmox_acl.kernel_modules.user_id == "ansible@pam" &&
      proxmox_acl.kernel_modules.role_id == "ScopedKernelModules" &&
      proxmox_acl.kernel_modules.propagate
    )
    error_message = "Grant ScopedKernelModules to ansible@pam on PowerEdge."
  }
}
