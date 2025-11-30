# GoShort - Secure & Cloud-Native URL Shortener 🚀

GoShort is a high-performance, containerized URL shortening microservice built with **Golang (Gin)**.  
This project demonstrates a full **DevSecOps lifecycle**, featuring a production-grade CI/CD pipeline with automated linting, security scanning (SAST), testing, and seamless deployment to a VPS using GitLab Runners.

---

## 🏗️ Architecture & Workflow

The project follows a **BYOR (Bring Your Own Runner)** architecture to ensure secure and efficient deployments.

graph LR
A[Developer] -->|Push Code| B(GitLab Repo)
B --> C{CI/CD Pipeline}
C -->|1. Quality| D[GolangCI-Lint]
C -->|2. Security| E[Gosec SAST]
C -->|3. Test| F[Go Test]
C -->|4. Build| G[Docker Registry]
C -->|5. Deploy| H[Production VPS]
H -->|Pull Image| G


---

## 🛠️ Tech Stack

- **Core**: Golang 1.21, Gin Framework
- **Containerization**: Docker (Multi-stage builds for <20MB images)
- **CI/CD**: GitLab CI, GitLab Runners (Self-hosted on VPS)
- **Security**: Gosec (SAST), GolangCI-Lint
- **Infrastructure**: Ubuntu VPS, SSH, Docker Engine

---

## 🚀 DevOps Pipeline Strategy

The `.gitlab-ci.yml` configuration implements a **5-stage pipeline** ensuring zero bad code reaches production:

1. **Quality** (`lint_code`): Enforces code standards using `golangci-lint`.
2. **Security** (`security_scan`): Scans for vulnerabilities (e.g., hardcoded secrets, unsafe functions) using `gosec`.
3. **Test** (`unit_test`): Runs functional unit tests.
4. **Build** (`build_push_docker`):
   - Builds a lightweight Alpine-based image.
   - Tags with both `commit-sha` (for rollback) and `latest`.
   - Pushes to GitLab Container Registry.
5. **Deploy** (`deploy_prod`):
   - Connects to VPS via SSH using a secure Runner.
   - Pulls the latest image.
   - Performs zero-downtime container replacement.
   - **Health Check**: Verifies the service is up via `curl` before marking success.

---

## 📦 How to Run Locally

If you want to run this project on your local machine:

### Prerequisites
- Docker & Docker Compose
- Go 1.21+

### Steps

1. **Clone the repository**:

git clone <repo-url>
cd goshort


2. **Run with Docker**:

docker build -t goshort:local .
docker run -p 8080:8080 goshort:local


3. **Verify**:
- Open browser at `http://localhost:8080`

{
"app": "GoShort",
"status": "healthy"
}


---

## 🔒 Security Measures

- **No Root User**: The container runs as a non-root process (best practice).
- **Minimal Base Image**: Uses `alpine` to reduce attack surface.
- **SSH Hardening**: Deployment uses SSH Keys (no passwords) and strict host checking configurations.
- **SAST Integration**: Every commit is scanned for CVEs before build.

---

## 🔮 Roadmap

- [ ] Integrate PostgreSQL for persistent storage.
- [ ] Implement Redis for caching hot URLs.
- [ ] Migrate from Docker Compose to Kubernetes (K8s) manifest files.
- [ ] Add Prometheus metrics endpoint.

---

**Author**: [Kagnite]  
Built with ❤️ and Go.
