# Running on AWS

```text
casino frontend
      | HTTPS / WSS
Route 53 + ACM certificate
      |
Application Load Balancer (public subnets, AWS WAF)
      |
ECS Fargate service, 2+ tasks across 2 availability zones (private subnets)
      |                         |                          |
RDS PostgreSQL Multi-AZ    ElastiCache Redis          NATS (when events are needed)
      |
S3 (archived ledger partitions)
```

| Piece | Role |
|---|---|
| Route 53 and ACM | domain and TLS certificate; clients only ever see HTTPS and WSS |
| Application Load Balancer | terminates TLS and supports WebSocket natively. Its idle timeout (60 s by default) is longer than the 30 s server ping, so idle game connections stay open. No sticky sessions: all state is in the database. Health check on `/healthz`. |
| AWS WAF and Shield | rate-based rules per IP and DDoS protection in front of the API |
| ECS Fargate | runs the Docker image from ECR. Deployments are rolling: ECS sends SIGTERM and the server shuts down gracefully within the task's stop timeout. Auto scaling on CPU and on active connections. |
| ECR | image registry |
| Secrets Manager | `DATABASE_URL` and other secrets, injected as environment variables into the task |
| RDS PostgreSQL, Multi-AZ | synchronous standby in a second zone, automated backups and point-in-time recovery; `pg_partman` keeps the ledger partitions ahead. A read replica serves reports and back office. |
| S3 | detached ledger months archived for the legal retention period, with Object Lock against deletion and a Glacier lifecycle for cost |
| ElastiCache Redis | rate limits shared by all tasks and a cache of tokens and operator configuration |
| NATS | balance and play events between tasks and to other systems, once there is a consumer for them; runs in the same private subnets |
| CloudWatch | the JSON logs from `slog`, with alarms on 5xx responses, `WALLET_UNAVAILABLE`, p99 latency and stuck plays |

Network and access:

- Tasks, database, Redis and NATS live in private subnets. Security groups only allow ALB to tasks and tasks to the database and Redis; the database is never public.
- The application connects as `dice_app` (no `DELETE`, append-only ledger). Migrations run with a separate owner role.
- Each task has an IAM role limited to reading its secrets and writing its logs.
