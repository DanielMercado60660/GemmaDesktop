"""Work out where each blog post is published."""
from datetime import date

from text_utils import slugify


def post_path(title: str, published: date) -> str:
    """Return the URL path for a post, e.g. "/2024/05/hello-world/"."""
    slug = slugify(title) or "untitled"
    return f"/{published:%Y/%m}/{slug}/"
