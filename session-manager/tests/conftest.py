import os
import sys

# Make the session-manager root importable so `handlers` resolves as an
# implicit namespace package (handlers has no __init__.py); its modules
# use relative imports like `from .helpers import xget`.
sys.path.insert(0, os.path.abspath(os.path.join(os.path.dirname(__file__), "..")))
