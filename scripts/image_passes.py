"""Image build policy: one pass by default, explicit two-pass reproducibility."""
import os


def validate_image_passes(value=None, environment=None):
    environment = os.environ if environment is None else environment
    raw = environment.get('IMAGE_PASSES', '1') if value is None else str(value)
    if raw not in ('1', '2'):
        raise ValueError('IMAGE_PASSES must be 1 or 2')
    return int(raw)
