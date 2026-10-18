# Deploy and restore Kea configuration

Deploy the repository's [DHCPv4 configuration](../../../kea/kea-dhcp4.conf)
and [DHCPv6 configuration](../../../kea/kea-dhcp6.conf) over SSH.
Restarting Kea briefly interrupts new address assignments and renewals.

## Prepare the router

1. Confirm the target router and SSH account against the current inventory.
   Keep console access available during the change. Review both configuration
   files against the target router's interfaces, subnets, and reservations.
2. In OPNsense, open **Services > Kea DHCP > Kea DHCPv4 > Settings** and the
   corresponding DHCPv6 page. Confirm that each service is enabled and uses
   **Manual config**. OPNsense can regenerate the files when manual mode is
   disabled. Export the current OPNsense configuration before changing these
   settings. The [OPNsense Kea manual](https://docs.opnsense.org/manual/kea.html)
   documents manual mode and the lease pages.
3. From the repository root, set the actual target and account. Use the host
   without brackets for SSH; the upload command adds brackets for IPv6.

   ```sh
   ROUTER_HOST='<router hostname or IPv6 address>'
   ROUTER_USER='<SSH account>'
   scp kea/kea-dhcp4.conf kea/kea-dhcp6.conf "${ROUTER_USER}@[${ROUTER_HOST}]:"
   ssh "${ROUTER_USER}@${ROUTER_HOST}"
   ```

## Validate and install the files

Run the following commands in the router's SSH session. Stop after any failed
command and resolve its error before continuing.

1. Validate both uploaded files with the router's installed binaries.

   ```sh
   sudo /usr/local/sbin/kea-dhcp4 -t "$HOME/kea-dhcp4.conf"
   sudo /usr/local/sbin/kea-dhcp6 -t "$HOME/kea-dhcp6.conf"
   ```

   The validation commands do not test live sockets or load hook libraries.
   The [DHCPv4 manual](https://kea.readthedocs.io/en/stable/man/kea-dhcp4.8.html)
   and [DHCPv6 manual](https://kea.readthedocs.io/en/stable/man/kea-dhcp6.8.html)
   explain these limits. Complete the service and client checks below.
2. Print the current UTC time and set `BACKUP_ID` to that time in
   `YYYYMMDD_HHMMSS` format. Retain this value for rollback.

   ```sh
   date -u '+%Y%m%d_%H%M%S'
   BACKUP_ID='<time printed above>'
   sudo cp -p /usr/local/etc/kea/kea-dhcp4.conf "/usr/local/etc/kea/kea-dhcp4.conf.backup.${BACKUP_ID}"
   sudo cp -p /usr/local/etc/kea/kea-dhcp6.conf "/usr/local/etc/kea/kea-dhcp6.conf.backup.${BACKUP_ID}"
   ```

3. Install the validated files with the existing owner and permissions.

   ```sh
   sudo install -o root -g wheel -m 640 "$HOME/kea-dhcp4.conf" /usr/local/etc/kea/kea-dhcp4.conf
   sudo install -o root -g wheel -m 640 "$HOME/kea-dhcp6.conf" /usr/local/etc/kea/kea-dhcp6.conf
   sudo ls -l /usr/local/etc/kea/kea-dhcp4.conf /usr/local/etc/kea/kea-dhcp6.conf
   ```

4. Restart Kea once, then inspect its service status and both lease lists.

   ```sh
   sudo configctl kea restart
   sudo configctl kea status
   sudo configctl kea list leases4
   sudo configctl kea list leases6
   ```

   The [OPNsense Kea actions](https://github.com/opnsense/core/blob/master/src/opnsense/service/conf/actions.d/actions_kea.conf)
   define these commands. Check the Kea log for startup or configuration
   errors. A successful restart command alone does not prove lease service.
5. Renew an IPv4 lease and an IPv6 lease from clients on the changed subnets.
   Compare the observed addresses, reservations, DNS settings, and lease
   updates with the intended configuration. Repeat the service and lease
   checks after renewal. Restore the backups if Kea cannot start or a client
   cannot obtain the intended lease.

## Restore the previous configuration

1. In the router's SSH session, set `BACKUP_ID` to the value retained during
   deployment. Validate both backup files before installing them.

   ```sh
   BACKUP_ID='<backup timestamp>'
   sudo /usr/local/sbin/kea-dhcp4 -t "/usr/local/etc/kea/kea-dhcp4.conf.backup.${BACKUP_ID}"
   sudo /usr/local/sbin/kea-dhcp6 -t "/usr/local/etc/kea/kea-dhcp6.conf.backup.${BACKUP_ID}"
   ```

2. Restore the files and restart Kea.

   ```sh
   sudo install -o root -g wheel -m 640 "/usr/local/etc/kea/kea-dhcp4.conf.backup.${BACKUP_ID}" /usr/local/etc/kea/kea-dhcp4.conf
   sudo install -o root -g wheel -m 640 "/usr/local/etc/kea/kea-dhcp6.conf.backup.${BACKUP_ID}" /usr/local/etc/kea/kea-dhcp6.conf
   sudo configctl kea restart
   ```

3. Repeat the service, log, lease, and client-renewal checks from deployment.
   Configuration rollback does not reverse lease changes made during the
   deployment. Investigate any client that still receives an unexpected lease.
