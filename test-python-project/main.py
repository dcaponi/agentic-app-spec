"""Grocery classification demo — runs the grocery-classify workflow."""

import asyncio
import json
import os
import sys

from generated.workflows.groceryClassify import grocery_classify, GroceryClassifyInput

# Set project root so the engine finds agents/workflows/routers here
project_dir = os.path.dirname(os.path.abspath(__file__))
os.chdir(project_dir)

# Load .env file
env_path = os.path.join(project_dir, ".env")
if os.path.exists(env_path):
    with open(env_path) as f:
        for line in f:
            line = line.strip()
            if line and not line.startswith("#") and "=" in line:
                key, _, value = line.partition("=")
                key, value = key.strip(), value.strip()
                if value and key not in os.environ:
                    os.environ[key] = value


async def classify(item_name: str) -> None:
    print(f"\nClassifying: {item_name}")
    print("-" * 40)

    result = await grocery_classify(GroceryClassifyInput(item_name=item_name))

    status = result.get("status", "unknown")
    output = result.get("result") or result.get("output")

    print(f"Status: {status}")
    print(json.dumps(output, indent=2))


async def main() -> None:
    items = (
        sys.argv[1:]
        if len(sys.argv) > 1
        else [
            "chicken breast",
            "paper towels",
            "canned tomatoes",
            "spinach",
        ]
    )

    for item in items:
        await classify(item)


if __name__ == "__main__":
    asyncio.run(main())
