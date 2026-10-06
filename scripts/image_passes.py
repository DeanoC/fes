"""Shared policy for diagnostic single-pass image builds."""
import os


def validate_image_passes(value=None, environment=None):
    environment = os.environ if environment is None else environment
    raw = environment.get('IMAGE_PASSES', '2') if value is None else str(value)
    if raw not in ('1', '2'):
        raise ValueError('IMAGE_PASSES must be 1 or 2')
    if raw == '1' and (environment.get('CI') == 'true' or environment.get('GITHUB_ACTIONS') == 'true'):
        raise ValueError('single-pass images are disabled in CI')
    return int(raw)
