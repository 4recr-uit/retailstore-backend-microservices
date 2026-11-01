![Banner](./docs/images/banner.png)

<div align="center">
  <div align="center">

[![Stars](https://img.shields.io/github/stars/aws-containers/retail-store-sample-app)](Stars)
![GitHub License](https://img.shields.io/github/license/aws-containers/retail-store-sample-app?color=green)
![Dynamic JSON Badge](https://img.shields.io/badge/dynamic/json?url=https%3A%2F%2Fraw.githubusercontent.com%2Faws-containers%2Fretail-store-sample-app%2Frefs%2Fheads%2Fmain%2F.release-please-manifest.json&query=%24%5B%22.%22%5D&label=release)
![GitHub Release Date](https://img.shields.io/github/release-date/aws-containers/retail-store-sample-app)

  </div>

  <strong>
  <h2>AWS Containers Retail Sample</h2>
  </strong>
</div>

This is a sample application designed to illustrate various concepts related to containers on AWS. It presents a sample retail store application including a product catalog, shopping cart and checkout.

### Worker reliability and readiness

Cart, Orders and Checkout acknowledge SQS batch entries individually. Retryable failed entries are retried at most three application attempts, preserving entry/job IDs; successful entries and sender-fault entries are not resent. If a batch remains incomplete, `/jobs` returns HTTP 502 with the confirmed `jobIds`, `failedJobIds`, and confirmed count (`count` for Cart/Orders, `enqueued` for Checkout). Publication metrics count confirmed entries only. A transport failure can leave delivery uncertain; clients must not blindly resubmit the whole request as new jobs.

Checkout receives no more messages than its processing capacity and waits for the current batch before polling again. Shutdown stops polling and drains active work before closing the executor. Cart and Checkout retain malformed/invalid messages without acknowledging them, allowing the queue's retry/DLQ policy to apply; fallback job IDs use stable SQS message IDs. Orders waits for worker completion before exiting, with `WORKER_SHUTDOWN_TIMEOUT_SECONDS` (default 60 seconds) as the drain limit. Kubernetes termination grace must cover drain and HTTP shutdown; messages not acknowledged before termination can be delivered again.

Cart, Orders and Checkout validate their existing PostgreSQL tables and required columns at startup using read-only queries. Catalog does the same for MySQL and never runs migrations or seed writes there. Missing tables/columns or missing SELECT access fail startup; runtime users do not need DDL privileges. Catalog's disposable in-memory mode still creates and seeds its SQLite store. Catalog and Recommendations expose `/ready` for database readiness; their charts use it for readiness probes. Recommendations checks that the catalog tables it reads exist and are accessible. `/health` continues to check process health.

For a new persistent database, provision it separately using [PostgreSQL setup](./database/postgres/setup.sql) and [MySQL setup](./database/mysql/setup.sql), then run the corresponding `verify.sql`. These setup scripts run once on a new database, not on every app restart or deployment. For local Compose, start `postgres` and `mysql` first, provision their databases through a database client, then start the backend services. Existing RDS tables and Catalog data are reused without modification. The optional Catalog test-container mode initializes its own disposable MySQL fixture explicitly; external MySQL test endpoints must already contain the schema and seed.

Run `go test ./...` within each Go service (`src/cart`, `src/orders`, `src/catalog`), and `python -m unittest discover -v` within each Python service (`src/checkout`, `src/recommendations`). Chart regression tests are in the repository-level `tests/` directory. These tests use mocks/in-memory databases; AWS IAM, RDS TLS and live scaling still require environment-level verification.

It provides:

- A demo store-front application with themes, pages to show container and application topology information, generative AI chat bot and utility functions for experimentation and demos.
- An optional distributed component architecture using various languages and frameworks
- A variety of different persistence backends for the various components like MariaDB (or MySQL), DynamoDB and Redis
- The ability to run in different container orchestration technologies like Docker Compose, Kubernetes etc.
- Pre-built container images for both x86-64 and ARM64 CPU architectures
- All components instrumented for Prometheus metrics and OpenTelemetry OTLP tracing
- Support for Istio on Kubernetes
- Load generator which exercises all of the infrastructure

See the [features documentation](./docs/features.md) for more information.

**This project is intended for educational purposes only and not for production use**

![Screenshot](/docs/images/screenshot.png)

## Application Architecture

The application has been deliberately over-engineered to generate multiple de-coupled components. These components generally have different infrastructure dependencies, and may support multiple "backends" (example: Carts service supports MongoDB or DynamoDB).

![Architecture](/docs/images/architecture.png)

| Component                  | Language | Container Image                                                             | Helm Chart                                                                        | Description                             |
| -------------------------- | -------- | --------------------------------------------------------------------------- | --------------------------------------------------------------------------------- | --------------------------------------- |
| [UI](./src/ui/)            | Java     | [Link](https://gallery.ecr.aws/aws-containers/retail-store-sample-ui)       | [Link](https://gallery.ecr.aws/aws-containers/retail-store-sample-ui-chart)       | Store user interface                    |
| [Catalog](./src/catalog/)  | Go       | [Link](https://gallery.ecr.aws/aws-containers/retail-store-sample-catalog)  | [Link](https://gallery.ecr.aws/aws-containers/retail-store-sample-catalog-chart)  | Product catalog API                     |
| [Cart](./src/cart/)        | Java     | [Link](https://gallery.ecr.aws/aws-containers/retail-store-sample-cart)     | [Link](https://gallery.ecr.aws/aws-containers/retail-store-sample-cart-chart)     | User shopping carts API                 |
| [Orders](./src/orders)     | Java     | [Link](https://gallery.ecr.aws/aws-containers/retail-store-sample-orders)   | [Link](https://gallery.ecr.aws/aws-containers/retail-store-sample-orders-chart)   | User orders API                         |
| [Checkout](./src/checkout) | Node     | [Link](https://gallery.ecr.aws/aws-containers/retail-store-sample-checkout) | [Link](https://gallery.ecr.aws/aws-containers/retail-store-sample-checkout-chart) | API to orchestrate the checkout process |

## Quickstart

The following sections provide quickstart instructions for various platforms.

### Docker

This deployment method will run the application as a single container on your local machine using `docker`.

Pre-requisites:

- Docker installed locally

Run the container:

```
docker run -it --rm -p 8888:8080 public.ecr.aws/aws-containers/retail-store-sample-ui:1.0.0
```

Open the frontend in a browser window:

```
http://localhost:8888
```

To stop the container in `docker` use Ctrl+C.

### Docker Compose

This deployment method will run the application on your local machine using `docker-compose`.

Pre-requisites:

- Docker installed locally

Download the latest Docker Compose file and use `docker compose` to run the application containers:

```
wget https://github.com/aws-containers/retail-store-sample-app/releases/latest/download/docker-compose.yaml

DB_PASSWORD='<some password>' docker compose --file docker-compose.yaml up
```

Open the frontend in a browser window:

```
http://localhost:8888
```

To stop the containers in `docker compose` use Ctrl+C. To delete all the containers and related resources run:

```
docker compose -f docker-compose.yaml down
```

### Kubernetes

This deployment method will run the application in an existing Kubernetes cluster.

Pre-requisites:

- Kubernetes cluster
- `kubectl` installed locally

Use `kubectl` to run the application:

```
kubectl apply -f https://github.com/aws-containers/retail-store-sample-app/releases/latest/download/kubernetes.yaml
kubectl wait --for=condition=available deployments --all
```

Get the URL for the frontend load balancer like so:

```
kubectl get svc ui
```

To remove the application use `kubectl` again:

```
kubectl delete -f https://github.com/aws-containers/retail-store-sample-app/releases/latest/download/kubernetes.yaml
```

### Terraform

The following options are available to deploy the application using Terraform:

| Name                                             | Description                                                                                                     |
| ------------------------------------------------ | --------------------------------------------------------------------------------------------------------------- |
| [Amazon EKS](./terraform/eks/default/)           | Deploys the application to Amazon EKS using other AWS services for dependencies, such as RDS, DynamoDB etc.     |
| [Amazon EKS (Minimal)](./terraform/eks/minimal/) | Deploys the application to Amazon EKS using in-cluster dependencies instead of RDS, DynamoDB etc.               |
| [Amazon ECS](./terraform/ecs/default/)           | Deploys the application to Amazon ECS using other AWS services for dependencies, such as RDS, DynamoDB etc.     |
| [AWS App Runner](./terraform/apprunner/)         | Deploys the application to AWS App Runner using other AWS services for dependencies, such as RDS, DynamoDB etc. |

### API and worker ServiceAccounts

The Cart, Orders, and Checkout charts use separate API and worker ServiceAccounts: `<service>-api` and `<service>-worker`. The infrastructure repository creates these accounts and associates their EKS Pod Identity roles; create them in the release namespace before deploying the charts. By default the charts reference these existing accounts without creating them. Set `apiServiceAccount.create` and/or `workerServiceAccount.create` to `true` only when Helm owns the corresponding account, and set each `.name` and `.annotations` independently if needed.

For existing Helm releases, the former shared `serviceAccount.*` values no longer control the API and worker identities. Migrate custom values to `apiServiceAccount.*` and `workerServiceAccount.*`, create the new accounts and associations, and then upgrade the release. The Pod Identity associations and account names must match in the `retailstore` namespace.

### Database Secrets

Database Secret creation and consumption are independent. For Cart, Orders, and Checkout, `database.secret.create` controls whether Helm creates the Secret; both API and worker Deployments always reference `database.secret.name`. An empty name defaults to `<fullname>-db`. For Recommendations, use `app.persistence.secret`; the Secret is created/referenced only when `app.persistence.provider` is `mysql`. Catalog already references `app.persistence.secret.name` when MySQL or search is enabled, regardless of `create`.

| Chart | Default Secret name | Required database keys in the Secret | Database username configuration |
|---|---|---|---|
| Cart | `cart-db` | `DB_PASSWORD` | `database.user` |
| Orders | `orders-db` | `DB_PASSWORD` | `database.user` |
| Checkout | `checkout-db` | `DB_PASSWORD` | `database.user` |
| Catalog | `catalog-db` | `RETAIL_CATALOG_PERSISTENCE_USER`, `RETAIL_CATALOG_PERSISTENCE_PASSWORD` | In the Secret |
| Recommendations | `recommendations-db` | `RETAIL_RECOMMENDATIONS_PERSISTENCE_PASSWORD` | `app.persistence.user` |

If Catalog search is enabled, the same Secret must also contain `RETAIL_CATALOG_SEARCH_OS_USERNAME` and `RETAIL_CATALOG_SEARCH_OS_PASSWORD`.

To use an existing Secret for Cart, put the following in the environment values for that chart:

```yaml
database:
  secret:
    create: false
    name: cart-db-external
```

For Catalog and Recommendations, configure the same `create` and `name` fields under `app.persistence.secret`. When using the application umbrella chart, nest each service's values under its alias (for example `cart.database.secret`). Recommendations is deployed separately from that umbrella chart.

Create each Secret in the release namespace before deploying. For example, from a local env file containing the key `DB_PASSWORD`:

```bash
kubectl create secret generic cart-db-external \
  --namespace retailstore \
  --from-env-file=/secure/local/cart-db.env
```

Keep real credential files outside version control. In existing-Secret mode, password values in the chart are not used to create database Secrets. If the referenced Secret is absent, Kubernetes cannot start the container; Helm rendering does not check that the Secret exists or that its credentials work.

When moving an existing release from a Helm-created Secret to an externally managed one, first provision a **different Secret name** and update the release to reference it with `create: false`. Simply disabling creation of the old Helm-owned Secret can remove it during upgrade. Secret values injected as environment variables are read when containers start; restart the affected workloads after changing credentials.

Run the local chart checks with:

```bash
helm lint src/cart/chart src/orders/chart src/checkout/chart src/catalog/chart src/recommendations/chart
python -m unittest discover -s tests -p 'test_*.py' -v
```

## Security

See [CONTRIBUTING](CONTRIBUTING.md#security-issue-notifications) for more information.

## License

This project is licensed under the MIT-0 License.

This package depends on and may incorporate or retrieve a number of third-party
software packages (such as open source packages) at install-time or build-time
or run-time ("External Dependencies"). The External Dependencies are subject to
license terms that you must accept in order to use this package. If you do not
accept all of the applicable license terms, you should not use this package. We
recommend that you consult your company’s open source approval policy before
proceeding.

Provided below is a list of External Dependencies and the applicable license
identification as indicated by the documentation associated with the External
Dependencies as of Amazon's most recent review.

THIS INFORMATION IS PROVIDED FOR CONVENIENCE ONLY. AMAZON DOES NOT PROMISE THAT
THE LIST OR THE APPLICABLE TERMS AND CONDITIONS ARE COMPLETE, ACCURATE, OR
UP-TO-DATE, AND AMAZON WILL HAVE NO LIABILITY FOR ANY INACCURACIES. YOU SHOULD
CONSULT THE DOWNLOAD SITES FOR THE EXTERNAL DEPENDENCIES FOR THE MOST COMPLETE
AND UP-TO-DATE LICENSING INFORMATION.

YOUR USE OF THE EXTERNAL DEPENDENCIES IS AT YOUR SOLE RISK. IN NO EVENT WILL
AMAZON BE LIABLE FOR ANY DAMAGES, INCLUDING WITHOUT LIMITATION ANY DIRECT,
INDIRECT, CONSEQUENTIAL, SPECIAL, INCIDENTAL, OR PUNITIVE DAMAGES (INCLUDING
FOR ANY LOSS OF GOODWILL, BUSINESS INTERRUPTION, LOST PROFITS OR DATA, OR
COMPUTER FAILURE OR MALFUNCTION) ARISING FROM OR RELATING TO THE EXTERNAL
DEPENDENCIES, HOWEVER CAUSED AND REGARDLESS OF THE THEORY OF LIABILITY, EVEN
IF AMAZON HAS BEEN ADVISED OF THE POSSIBILITY OF SUCH DAMAGES. THESE LIMITATIONS
AND DISCLAIMERS APPLY EXCEPT TO THE EXTENT PROHIBITED BY APPLICABLE LAW.

MariaDB Community License - [LICENSE](https://mariadb.com/kb/en/mariadb-licenses/)
MySQL Community Edition - [LICENSE](https://github.com/mysql/mysql-server/blob/8.0/LICENSE)
