"""Create, wait for, and delete Preview CRs for experiments."""
import json
import os
import subprocess
import time
import uuid
from typing import Optional


def _kubectl(*args, check=True, capture=True) -> subprocess.CompletedProcess:
    cmd = ["kubectl", *args]
    return subprocess.run(
        cmd,
        check=check,
        capture_output=capture,
        text=True,
    )


def runtime_namespace(pr_number: int) -> str:
    """Return the namespace the operator creates for a given PR number."""
    return f"preview-pr-{pr_number}"


def create(
    name: str,
    cr_namespace: str,
    image: str = None,
    branch: str = "main",
    pr_number: int = 1,
    isolation_enabled: bool = True,
    extra_spec: Optional[dict] = None,
    subject: Optional[dict] = None,
    subject_image: Optional[str] = None,
    probe_image: Optional[str] = None,
) -> str:
    """
    Create a Preview CR and return its name.

    When *subject* is provided (a dict loaded from meta.yaml), services and
    migration are built from the subject definition.  *image* is still used
    as the top-level spec.image (for backward-compatible single-image mode).

    When *subject* is None the original S1 hard-coded service layout is used.
    """
    if subject is not None:
        services = _build_services(subject, subject_image or image, probe_image or "")
        migration_cmd = subject.get("migration_command", ["alembic", "upgrade", "head"])
        top_image = subject_image or image
    else:
        services = [
            {
                "name": "backend",
                "image": image,
                "port": 8080,
                "pathPrefix": "/api",
            },
            {
                "name": "frontend",
                "image": image,
                "port": 3000,
                "pathPrefix": "/",
                "env": [
                    {"name": "APP_MODE",    "value": "frontend"},
                    {"name": "BACKEND_URL", "value": "http://svc-backend:8080"},
                ],
            },
        ]
        migration_cmd = ["alembic", "upgrade", "head"]
        top_image = image

    manifest = {
        "apiVersion": "platform.company.io/v1alpha1",
        "kind": "Preview",
        "metadata": {"name": name, "namespace": cr_namespace},
        "spec": {
            "branch": branch,
            "prNumber": pr_number,
            "image": top_image,
            "ttl": "2h",
            "resourceTier": "medium",
            "services": services,
            "database": {
                "enabled": True,
                "version": "15",
                "isolationEnabled": isolation_enabled,
                # PHASE B (RQ3 baseline) — operator picks isolation mechanism
                # from spec.database.isolationMode. Defaults to "restore"
                # (pg_dump+psql restore, current behavior). Set CHECKPOINT_MODE
                # env var to "migration" to use the migration-replay baseline.
                "isolationMode": os.environ.get("CHECKPOINT_MODE", "restore"),
                "migration": {
                    "enabled": True,
                    "command": migration_cmd,
                },
            },
            "testSuite": {
                "enabled": True,
                "smoke":      {"enabled": True},
                "regression": {"enabled": True},
                "e2e":        {"enabled": True},
            },
        },
    }
    if extra_spec:
        _deep_merge(manifest["spec"], extra_spec)

    subprocess.run(
        ["kubectl", "apply", "-f", "-"],
        input=json.dumps(manifest),
        capture_output=True,
        text=True,
        check=True,
    )
    return name


def _build_services(subject: dict, app_img: str, probe_img: str) -> list:
    """Translate meta.yaml services[] into Preview CR spec.services[]."""
    result = []
    for svc in subject.get("services", []):
        key = svc.get("image_key", "custom")
        if key == "app":
            img = app_img
        elif key == "probe":
            img = probe_img
        else:
            img = svc.get("image", app_img)

        entry: dict = {
            "name":       svc["name"],
            "image":      img,
            "port":       svc["port"],
            "pathPrefix": svc.get("path_prefix", "/"),
        }
        if svc.get("env"):
            entry["env"] = svc["env"]
        result.append(entry)
    return result


def wait_until_phase(
    name: str,
    namespace: str,
    target_phases: list[str],
    timeout_s: int = 1200,
    poll_interval_s: int = 5,
) -> str:
    """Block until the Preview reaches one of target_phases. Returns final phase."""
    deadline = time.monotonic() + timeout_s
    while time.monotonic() < deadline:
        phase = get_phase(name, namespace)
        if phase in target_phases:
            return phase
        time.sleep(poll_interval_s)
    raise TimeoutError(
        f"Preview {namespace}/{name} did not reach {target_phases} within {timeout_s}s"
    )


def wait_until_tests_done(
    name: str,
    namespace: str,
    timeout_s: int = 1200,
    poll_interval_s: int = 5,
) -> str:
    """Block until status.tests.phase is Succeeded or Failed. Returns final tests phase."""
    deadline = time.monotonic() + timeout_s
    while time.monotonic() < deadline:
        result = _kubectl(
            "get", "preview", name, "-n", namespace,
            "-o", "jsonpath={.status.tests.phase}",
        )
        phase = result.stdout.strip()
        if phase in ("Succeeded", "Failed"):
            return phase
        time.sleep(poll_interval_s)
    raise TimeoutError(
        f"Preview {namespace}/{name} tests did not complete within {timeout_s}s"
    )


def sut_deterministically_broken(runtime_namespace: str) -> bool:
    """Return True when a pod in the preview's runtime namespace has a
    container stuck in CrashLoopBackOff with restartCount >= 3, or in an
    image-pull error. Such a system-under-test will never become Ready,
    so any test suite run against it would fail — which, in a mutation
    testing context, means the injected mutant is detected. Detecting
    this state lets the caller return a verdict in ~1-2 min instead of
    waiting out the full preview timeout.
    """
    result = _kubectl(
        "get", "pods", "-n", runtime_namespace, "-o", "json", check=False,
    )
    if result.returncode != 0:
        return False
    try:
        pods = json.loads(result.stdout).get("items", [])
    except (json.JSONDecodeError, ValueError):
        return False
    for pod in pods:
        statuses = (pod.get("status", {}).get("containerStatuses", []) or [])
        statuses += (pod.get("status", {}).get("initContainerStatuses", []) or [])
        for cs in statuses:
            waiting = (cs.get("state", {}) or {}).get("waiting", {}) or {}
            reason = waiting.get("reason", "")
            restarts = cs.get("restartCount", 0) or 0
            if reason == "CrashLoopBackOff" and restarts >= 3:
                return True
            if reason in ("ImagePullBackOff", "ErrImagePull") and restarts >= 1:
                return True
    return False


def wait_until_running_or_broken(
    name: str,
    cr_namespace: str,
    runtime_namespace_: str,
    timeout_s: int = 1200,
    poll_interval_s: int = 5,
) -> str:
    """Block until the Preview reaches Running or Failed, OR the SUT pod
    becomes deterministically broken (CrashLoopBackOff / image-pull
    error). Returns one of: "Running", "Failed", "SUTBroken". Raises
    TimeoutError only if none of these states is reached in time.
    """
    deadline = time.monotonic() + timeout_s
    while time.monotonic() < deadline:
        phase = get_phase(name, cr_namespace)
        if phase in ("Running", "Failed"):
            return phase
        if sut_deterministically_broken(runtime_namespace_):
            return "SUTBroken"
        time.sleep(poll_interval_s)
    raise TimeoutError(
        f"Preview {cr_namespace}/{name} did not reach Running/Failed/"
        f"SUTBroken within {timeout_s}s"
    )


def get_phase(name: str, namespace: str) -> str:
    # Same resilience pattern as get_tests_step (webhook transient during operator restarts).
    for _ in range(5):
        result = _kubectl(
            "get", "preview", name, "-n", namespace,
            "-o", "jsonpath={.status.phase}",
            check=False,
        )
        if result.returncode == 0:
            return result.stdout.strip() or "Unknown"
        stderr_low = (result.stderr or "").lower()
        if "notfound" in stderr_low or "not found" in stderr_low:
            return "NotFound"
        time.sleep(2)
    return "Unknown"


def get_status(name: str, namespace: str) -> dict:
    result = _kubectl(
        "get", "preview", name, "-n", namespace, "-o", "json",
    )
    return json.loads(result.stdout).get("status", {})


def get_tests_step(name: str, namespace: str) -> str:
    # Resilient against transient webhook unavailability during operator restarts
    # (idempotence experiments deliberately kill the operator pod).
    for _ in range(5):
        result = _kubectl(
            "get", "preview", name, "-n", namespace,
            "-o", "jsonpath={.status.tests.step}",
            check=False,
        )
        if result.returncode == 0:
            return result.stdout.strip()
        stderr_low = (result.stderr or "").lower()
        if "notfound" in stderr_low or "not found" in stderr_low:
            return ""
        time.sleep(2)
    return ""


def delete(name: str, namespace: str, wait: bool = True) -> None:
    _kubectl("delete", "preview", name, "-n", namespace, "--ignore-not-found=true")
    if wait:
        _kubectl(
            "wait", "--for=delete",
            f"preview/{name}", "-n", namespace,
            "--timeout=120s",
            check=False,
        )


def unique_name(prefix: str) -> str:
    return f"{prefix}-{uuid.uuid4().hex[:8]}"


def _deep_merge(base: dict, override: dict) -> None:
    for k, v in override.items():
        if k in base and isinstance(base[k], dict) and isinstance(v, dict):
            _deep_merge(base[k], v)
        else:
            base[k] = v
