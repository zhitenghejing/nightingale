# MySQL 8.4 LTS remote monitoring

Select Database / MySQL / 8.4 LTS in the device configuration wizard. Enter the database IP, monitoring username/password and port, then choose an authorized Categraf collector. No local target configuration is required. The same collector can monitor multiple independent endpoints.

The typed recipe gathers global status and global variables. Use a monitoring account with the permissions required by these queries; schema/table/process-list/replication collection is disabled unless explicitly enabled in the package. Passwords are encrypted and never returned by device read APIs. TLS verification uses the collector host trust store and requires a certificate valid for the selected IP.

Required metrics: mysql_up, mysql_global_status_threads_connected and mysql_global_variables_max_connections. The package also documents query count, slow queries, running threads and uptime. Each device receives an independent dashboard and native alert rules. Rules wait for the complete first metric contract; the default notification mode creates events only. Select an authorized native notification rule when delivery is required.

Compatibility evidence: MySQL 8.4.11, pinned Categraf v0.5.18, authenticated HTTP Provider and managed Remote Write fixture. This is not evidence for other database versions or physical hardware.

Edit the complete package in Template center, publish it, then use the three-level selectors in the device configuration wizard. The bootstrap declaration is maintained in the source-language README; it is not a runtime local collector configuration.
