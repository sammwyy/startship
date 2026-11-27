# App and service catalogs

Startship bundles [recipes.json](../registry/recipes.json) and
[services.json](../registry/services.json). The default remote sources are
the raw versions of those files on the `sammwyy/startship` `main` branch.
To use your own sources, set `STARTSHIP_RECIPES_URL` and/or
`STARTSHIP_SERVICES_URL` in the daemon environment (in the daemon service's
`environment` block with Docker Compose), then restart it. Only use trusted
sources: they control the images and configuration offered for new installs.

Opening a catalog starts a background refresh if its local cache is missing
or at least 72 hours old. Startship validates each download before replacing
the entire local catalog, saves it in the daemon's data directory, and pushes
the update to open dashboards. If the download fails, Startship keeps the
last valid cache, or uses the bundled JSON when there is no valid cache.

Use **Refresh catalog** in the install/deploy dialog to fetch immediately.
The dialog shows the source and last successful refresh time. Platform
administrators can also run `startship apps recipes refresh` or
`startship catalog refresh`; include a workspace name or ID to use that
workspace's `apps.manage` or `services.manage` permission, respectively.

A services index is an object with an `engines` array (see the bundled file
for examples). Entries define engine IDs, image repositories and version tags,
ports, data paths, display metadata, and config fields. Engine-specific
credential validation, environment mapping, commands and connection strings
remain in the daemon. Existing installations retain their saved definitions;
a catalog refresh alone does not recreate containers or change their settings.
