from dataclasses import dataclass
from typing import List

STATUSES = ("open", "in_progress", "done")


@dataclass(frozen=True)
class Task:
    id: int
    title: str
    status: str
    priority: int
    created_at: str


@dataclass(frozen=True)
class TaskPage:
    items: List[Task]
    total: int
    limit: int
    offset: int
