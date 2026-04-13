"""Example of browsing the skills catalog."""

import os

from shannon import ShannonClient


def main() -> None:
    base_url = os.getenv("SHANNON_BASE_URL", "http://localhost:8080")
    api_key = os.getenv("SHANNON_API_KEY")
    selected_skill = os.getenv("SHANNON_SKILL_NAME")

    client = ShannonClient(base_url=base_url, api_key=api_key)
    try:
        skills = client.list_skills()
        print(f"Skills: {len(skills)}")
        for skill in skills[:10]:
            print(f"  - {skill.name} {skill.version} ({skill.category})")

        if not skills:
            return

        skill_name = selected_skill or skills[0].name
        detail = client.get_skill(skill_name)
        versions = client.get_skill_versions(skill_name)

        print()
        print(f"Skill: {detail.name}")
        print(f"Description: {detail.description}")
        print(f"Requires tools: {detail.requires_tools}")
        print(f"Known versions: {[version.version for version in versions]}")
    finally:
        client.close()


if __name__ == "__main__":
    main()
