#!/usr/bin/env python
import json

import requests

golang_tmpl = """package stashvision

var PoeItemClassesToNames = make(map[string][]string)
var PoeItemNamesToClasses = make(map[string]string)

func init() {{
    {init_body}
    for itemClass, itemNames := range PoeItemClassesToNames {{
        for _, itemName := range itemNames {{
            PoeItemNamesToClasses[itemName] = itemClass
        }}
    }}
}}

"""
item_class_names = (
    "Amulet",
    "Belt",
    "Boots",
    "Bow",
    "Body Armour",
    "Claw",
    "Dagger",
    "Gloves",
    "Helmet",
    "One Hand Axe",
    "One Hand Mace",
    "One Hand Sword",
    "Quiver",
    "Ring",
    "Sceptre",
    "Shield",
    "Staff",
    "Two Hand Axe",
    "Two Hand Mace",
    "Two Hand Sword",
    "Wand",
    "Warstaff",
)
repoe_repo = "brather1ng/RePoE"
base_items_url = f"https://github.com/{repoe_repo}/raw/master/data/base_items.json"


def main():
    r = requests.get(base_items_url)
    base_items = r.json()
    item_classes = {k: set() for k in item_class_names}
    for key, meta in base_items.items():
        try:
            item_classes[meta["item_class"]].add(meta["name"])
        except KeyError:
            continue
    init_statements = []
    for class_name, names in item_classes.items():
        names_repr = json.dumps(list(names), indent=8, ensure_ascii=False)
        init_statements.append(
            f'PoeItemClassesToNames["{class_name}"] = '
            f"[]string{{\n        {names_repr[1:-1].strip()},\n    }}"
        )
    with open("./stashvision-go/item_classes.go", "w") as f:
        f.write(golang_tmpl.format(init_body="\n    ".join(init_statements)))
    # with open(filename, "w") as f:
    #     json.dump(item_types, f, indent=4, ensure_ascii=False)


if __name__ == "__main__":
    main()
