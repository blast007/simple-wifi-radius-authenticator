# Simple WiFi RADIUS Authenticator

The purpose is to create a simplified RADIUS server suitable for WiFi controller MAC address allowlist/denylist operation. It will be easier to set up and maintain compared to the Windows Server Network Policy Server or FreeRADIUS.

## Status

This is very much alpha quality software. I would not recommend using this in production until it is further along. This is my first Go language project so I'm still learning best practices.

## ToDo
[X] MAC address normalization
[X] SQLite storage
[X] Device groups
[X] Access permissions for groups
[ ] RADIUS client settings (password mode and RADIUS secret)
[ ] Unknown/guest device support
[ ] Web UI
[ ] Command line data manipulation?
