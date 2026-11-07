"""Read changed-file selections without putting large lists in argv or env."""
import json
from pathlib import Path


def add_arguments(parser, *, help):
    group = parser.add_mutually_exclusive_group()
    group.add_argument('--paths-json', help=help)
    group.add_argument('--paths-json-file', type=Path,
                       help='file containing the same JSON array of changed paths')


def read(args):
    if args.paths_json is None and args.paths_json_file is None:
        return None
    payload = (args.paths_json_file.read_text() if args.paths_json_file is not None
               else args.paths_json)
    paths = json.loads(payload)
    if not isinstance(paths, list) or not all(isinstance(path, str) for path in paths):
        raise ValueError('changed paths must be a JSON array of strings')
    return paths
