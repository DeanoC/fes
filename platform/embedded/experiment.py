"""Explicit build selection for the unfinished Linux-free DE25 experiment."""
import os

VARIABLE = "FES_EXPERIMENTAL_DE25_EMBEDDED"


def require_experiment(environ=None):
    env = os.environ if environ is None else environ
    if env.get(VARIABLE) != "1":
        raise ValueError(
            "Linux-free DE25 builds are experimental; explicitly set "
            + VARIABLE + "=1 (see platform/embedded/README.md)"
        )


if __name__ == "__main__":
    try:
        require_experiment()
    except ValueError as error:
        raise SystemExit(str(error))
