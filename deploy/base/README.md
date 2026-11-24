# Base manifests

Plain Kubernetes YAML for the shop, one file per service plus shared objects.

| File | Contents |
|---|---|
| `storefront.yaml`, `catalog.yaml`, `cart.yaml`, `checkout.yaml`, `payments-mock.yaml` | Deployment and Service for each service |
| `configmap.yaml` | `shop-config`: environment shared by all services |
| `ingress.yaml` | Ingress for the storefront (host `SHOP_HOST`) |
| `migrate-job.yaml` | Job that creates the schema and seed data (`catalog migrate`) |

Placeholders to replace: `REPLACE_ME` (container image) and `SHOP_HOST` (ingress host).

## Required Secrets

| Secret | Key | Used for |
|---|---|---|
| `shop-postgres` | `url` | `DATABASE_URL` for catalog, checkout and the migrate Job |
| `shop-redis` | `url` | A Redis connection URL, for services that need one |

Both Secrets must exist in the namespace before the workloads start.
