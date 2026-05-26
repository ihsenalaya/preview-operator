"""Load experiment configuration from config.yaml, with env-var overrides (EXP_*)."""
import os
import pathlib
import yaml

_ROOT = pathlib.Path(__file__).parent.parent
_CONFIG_PATH = _ROOT / "config.yaml"


def load() -> dict:
    with open(_CONFIG_PATH) as f:
        cfg = yaml.safe_load(f)
    _apply_env_overrides(cfg, "EXP")
    return cfg


def load_subject(subject_id: str) -> dict:
    """Load subjects/{subject_id}/meta.yaml and return its parsed content."""
    meta_path = _ROOT / "subjects" / subject_id / "meta.yaml"
    with open(meta_path) as f:
        return yaml.safe_load(f)


def load_enabled_subjects(cfg: dict) -> list[dict]:
    """Return meta.yaml dicts for each subject listed in cfg['subjects']['enabled']."""
    enabled = cfg.get("subjects", {}).get("enabled", ["s1-flask-catalog"])
    return [load_subject(sid) for sid in enabled]


def subject_image(cfg: dict, subject_id: str) -> str:
    """Return the adapter image for a subject, falling back to cfg['app']['image']."""
    return (cfg.get("subjects", {})
               .get("images", {})
               .get(subject_id, cfg["app"]["image"]))


def probe_image(cfg: dict) -> str:
    return cfg.get("subjects", {}).get("probe_image", "")


def _apply_env_overrides(obj: dict, prefix: str) -> None:
    for key, val in obj.items():
        env_key = f"{prefix}_{key.upper()}"
        if env_key in os.environ:
            obj[key] = _coerce(os.environ[env_key], type(val))
        elif isinstance(val, dict):
            _apply_env_overrides(val, env_key)


def _coerce(raw: str, target_type):
    if target_type is bool:
        return raw.lower() in ("1", "true", "yes")
    if target_type is int:
        return int(raw)
    if target_type is float:
        return float(raw)
    return raw
