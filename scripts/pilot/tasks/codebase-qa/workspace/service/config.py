"""Application-wide constants for the notes service."""

APP_NAME = "notes-service"
API_PREFIX = "/api/v2"

# Storage
DEFAULT_DATA_FILE = "data/notes.json"
MAX_TITLE_LENGTH = 200
MAX_NOTE_LENGTH = 10_000

# Listing
PAGE_SIZE = 50
MAX_PAGE_SIZE = 200

# Requests with a larger body are rejected before they are parsed.
MAX_BODY_BYTES = 64 * 1024
