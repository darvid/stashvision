package stashvision

import (
	"errors"
	"fmt"
	"github.com/blevesearch/bleve"
	log "github.com/sirupsen/logrus"
	"strings"
)

var RecipeFactories = make(map[string]RecipeFactory)

func RegisterRecipe(name string, factory RecipeFactory) {
	ctxLogger := log.WithFields(log.Fields{"name": name})
	if factory == nil {
		ctxLogger.Fatalln("recipe factory does not exist")
	}
	if _, registered := RecipeFactories[name]; registered {
		ctxLogger.Error("recipe factory already registered")
		return
	}
	RecipeFactories[name] = factory
}

type Recipe interface {
	ScanIndex(item *PoeStashItem, tabIndex int, index bleve.Index, findAll bool) (results []RecipeResult, err error)
}

type Currency struct {
	Value int
	Name  string
}

type RecipeResult struct {
	Items  []PoeStashItem
	Reward Reward
}

type Reward struct {
	Currency *Currency
	Item     *string
}

type RecipeFactory func() (Recipe, error)

const (
	ChaosRecipeMinItemLevel = 60
	ChaosRecipeMaxItemLevel = 74
)

func IsClassTwoHandedWeapon(class string) (bool, error) {
	switch class {
	case "Claw", "Dagger", "One Hand Axe", "One Hand Mace",
		"One Hand Sword", "Sceptre", "Shield", "Wand":
		return false, nil
	case "Bow", "Staff", "Two Hand Axe", "Two Hand Mace",
		"Two Hand Sword", "Warstaff":
		return true, nil
	}
	return false, errors.New("item class is not a weapon class")
}

const (
	ArmourCapacity = 7
	WeaponCapacity = 2
)

type ItemSet struct {
	ClassToItems               map[string][]PoeStashItem
	MinItemLevel, MaxItemLevel int
	armourCount, weaponCount   int
}

func NewItemSet(minItemLevel int, maxItemlevel int) *ItemSet {
	return &ItemSet{
		ClassToItems: map[string][]PoeStashItem{
			"Amulet":         make([]PoeStashItem, 0, 1),
			"Ring":           make([]PoeStashItem, 0, 2),
			"Belt":           make([]PoeStashItem, 0, 1),
			"Boots":          make([]PoeStashItem, 0, 1),
			"Gloves":         make([]PoeStashItem, 0, 1),
			"Body Armour":    make([]PoeStashItem, 0, 1),
			"Helmet":         make([]PoeStashItem, 0, 1),
			"Bow":            make([]PoeStashItem, 0, 1),
			"Claw":           make([]PoeStashItem, 0, 2),
			"Dagger":         make([]PoeStashItem, 0, 2),
			"One Hand Axe":   make([]PoeStashItem, 0, 2),
			"One Hand Mace":  make([]PoeStashItem, 0, 2),
			"One Hand Sword": make([]PoeStashItem, 0, 2),
			"Sceptre":        make([]PoeStashItem, 0, 2),
			"Shield":         make([]PoeStashItem, 0, 1),
			"Staff":          make([]PoeStashItem, 0, 1),
			"Two Hand Axe":   make([]PoeStashItem, 0, 1),
			"Two Hand Mace":  make([]PoeStashItem, 0, 1),
			"Two Hand Sword": make([]PoeStashItem, 0, 1),
			"Wand":           make([]PoeStashItem, 0, 2),
			"Warstaff":       make([]PoeStashItem, 0, 1),
		},
		MinItemLevel: minItemLevel,
		MaxItemLevel: maxItemlevel,
	}
}

func (s *ItemSet) IsFull() bool {
	return s.armourCount == ArmourCapacity && s.weaponCount == WeaponCapacity
}

func (s *ItemSet) AddStashItem(item PoeStashItem, weaponGreedy bool) error {
	if item.ItemLevel < s.MinItemLevel || item.ItemLevel > s.MaxItemLevel {
		return errors.New("invalid item level")
	}
	if classItems, ok := s.ClassToItems[item.Class]; !ok {
		return errors.New("invalid item class")
	} else {
		if len(classItems) == cap(classItems) {
			return errors.New("item class already at capacity")
		}
		if twohanded, err := IsClassTwoHandedWeapon(item.Class); err != nil {
			// Armour
			if s.armourCount == ArmourCapacity {
				return errors.New("armour at capacity")
			}
			s.armourCount++
		} else {
			if s.weaponCount == WeaponCapacity {
				return errors.New("weapons at capacity")
			}
			if s.weaponCount == 1 && twohanded {
				// one-handed weapon already in set
				if weaponGreedy {
					s.RemoveAllWeapons()
				} else {
					return errors.New("weapon is 2h, looking for 1h")
				}
			}
			s.weaponCount++
			if twohanded {
				s.weaponCount++
			}
		}
		s.ClassToItems[item.Class] = append(classItems, item)
	}
	return nil
}

func (s *ItemSet) RemoveAllWeapons() {
	for class, itemSet := range s.ClassToItems {
		if _, err := IsClassTwoHandedWeapon(class); err != nil {
			s.ClassToItems[class] = itemSet[:0]
		}
	}
	s.weaponCount = 0
}

func (s *ItemSet) Items() (items []PoeStashItem) {
	for _, setItems := range s.ClassToItems {
		for _, item := range setItems {
			items = append(items, item)
		}
	}
	return
}

type UnidChaosRecipe struct{}

func NewUnidChaosRecipe() (Recipe, error) {
	return &UnidChaosRecipe{}, nil
}

func getViableRares(tabIndex int, index bleve.Index, searchSize int) (items []PoeStashItem, err error) {
	var filters []string
	if tabIndex != -1 {
		filters = append(filters, fmt.Sprintf("tabIndex:%d", tabIndex))
	}
	filters = append(filters, fmt.Sprintf("frameType:%d", poeFrameTypes[PoeFrameTypeRare]))
	filters = append(filters, fmt.Sprintf("itemLevel:>=%d", ChaosRecipeMinItemLevel))
	filters = append(filters, fmt.Sprintf("itemLevel:<=%d", ChaosRecipeMaxItemLevel))
	querystring := strings.Join(filters, " ")
	return QueryIndex(querystring, index, searchSize)
}

func (c *UnidChaosRecipe) ScanIndex(targetItem *PoeStashItem, tabIndex int, index bleve.Index, findAll bool) (results []RecipeResult, err error) {
	// if targetItem == nil {
	// 	rares, err := getViableRares(tabIndex, index, 1)
	// 	if err != nil {
	// 		return results, err
	// 	}
	// 	if len(rares) == 0 {
	// 		return results, errors.New(fmt.Sprintf("stash tab %d is empty", tabIndex))
	// 	}
	// 	targetItem = &rares[0]
	// }
	// if targetItem.ItemLevel < ChaosRecipeMinItemLevel || targetItem.ItemLevel > ChaosRecipeMaxItemLevel {
	// 	return results, errors.New("item level not between 60-74")
	// }
	stash, err := getViableRares(tabIndex, index, PoeQuadTabSize)
	if err != nil {
		return results, err
	}
	for len(stash) > 1 {
		set := NewItemSet(ChaosRecipeMinItemLevel, ChaosRecipeMaxItemLevel)
		if targetItem != nil {
			err = set.AddStashItem(*targetItem, true)
			if err != nil {
				return results, errors.New("invalid target item")
			}
		}
		nextStash := stash[:0]
		for index, stashItem := range stash {
			ctxLogger := log.WithFields(log.Fields{
				"itemClass": stashItem.Class,
			})
			err = set.AddStashItem(stashItem, true)
			if err != nil {
				ctxLogger.Debug(err)
				nextStash = append(nextStash, stashItem)
				continue
			}
			if set.IsFull() {
				nextStash = append(nextStash, stash[index+1:]...)
				break
			}
		}
		if !set.IsFull() {
			break
		}
		stash = nextStash
		results = append(results, RecipeResult{
			Items: set.Items(),
			Reward: Reward{
				Currency: &Currency{2, "Chaos Orb"},
			}})
		if !findAll {
			break
		}
	}
	return results, nil
}

func init() {
	RegisterRecipe("unid_chaos", NewUnidChaosRecipe)
}
