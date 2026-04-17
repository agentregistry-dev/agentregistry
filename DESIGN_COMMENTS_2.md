File: design-docs/UNIFIED_API_REFACTOR.md:15
Author: @shashankram
Comment: Should the status be stored in a separate jsonb column so that it can be updated without needing to pass the whole spec? 
---

File: design-docs/UNIFIED_API_REFACTOR.md:19
Author: @shashankram
Comment: why is it called TemplateRef? TargetRef/ResourceRef is a bit clearer (to me)
---

File: design-docs/UNIFIED_API_REFACTOR.md:80
Author: @shashankram
Comment: is this is_latest_version?
---

File: design-docs/UNIFIED_API_REFACTOR.md:70
Author: @shashankram
Comment: We need to preserve the mcp_backends table for now. It will be used to translate agentgateway config. We can rename it, but it needs to exist.

Also, agents deployed from the registry need to store their per platform identities/principals in some table. We could have a separate table to do this
---

File: design-docs/UNIFIED_API_REFACTOR.md:17
Author: @yuval-k
Comment: Why are we exposing Generation as a user facing field?
---

File: design-docs/UNIFIED_API_REFACTOR.md:15
Author: @EItanya
Comment: If we throw status and spec into JSON blobs it may make it harder to do foreign keys and relationships. Are we doing this to have one generic DB type?
---

File: design-docs/UNIFIED_API_REFACTOR.md:16
Author: @EItanya
Comment: Do we have a top-level breakdown concept, like "tenants", "namespaces", "projects", etc.
---

File: design-docs/UNIFIED_API_REFACTOR.md:57
Author: @EItanya
Comment: I think we need DeletedAt or some deletion marker so we can properly clean up
---

File: design-docs/UNIFIED_API_REFACTOR.md:85
Author: @EItanya
Comment: What's contained in these JSON blobs?
---

File: design-docs/UNIFIED_API_REFACTOR.md:79
Author: @EItanya
Comment: Same question about this being typed above
---

File: design-docs/UNIFIED_API_REFACTOR.md:16
Author: @shashankram
Comment: We are eventually going to need namespaces, so it would be good to account for that in the schema
---

File: design-docs/UNIFIED_API_REFACTOR.md:85
Author: @shashankram
Comment: {name,version} ref
---

