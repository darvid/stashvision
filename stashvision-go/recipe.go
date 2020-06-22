package stashvision

import (
	"errors"
	"fmt"
	"strings"

	"github.com/blevesearch/bleve"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
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
		"One Hand Sword", "Rune Dagger", "Sceptre", "Shield",
		"Thrusting One Hand Sword", "Wand":
		return false, nil
	case "Bow", "Staff", "Two Hand Axe", "Two Hand Mace",
		"Two Hand Sword", "Warstaff":
		return true, nil
	}
	return false, errors.New("item class is not a weapon class")
}

const (
	ArmourCapacity = 8
	WeaponCapacity = 2
)

type ItemSet struct {
	UUID                       uuid.UUID
	ClassToItems               map[string][]PoeStashItem
	MinItemLevel, MaxItemLevel int
	armourCount, weaponCount   int
}

func NewItemSet(minItemLevel int, maxItemlevel int) (*ItemSet, error) {
	uuid, err := uuid.NewUUID()
	if err != nil {
		return nil, err
	}
	return &ItemSet{
		UUID: uuid,
		ClassToItems: map[string][]PoeStashItem{
			"Amulet":                   make([]PoeStashItem, 0, 1),
			"Ring":                     make([]PoeStashItem, 0, 2),
			"Belt":                     make([]PoeStashItem, 0, 1),
			"Boots":                    make([]PoeStashItem, 0, 1),
			"Gloves":                   make([]PoeStashItem, 0, 1),
			"Body Armour":              make([]PoeStashItem, 0, 1),
			"Helmet":                   make([]PoeStashItem, 0, 1),
			"Bow":                      make([]PoeStashItem, 0, 1),
			"Claw":                     make([]PoeStashItem, 0, 2),
			"Dagger":                   make([]PoeStashItem, 0, 2),
			"One Hand Axe":             make([]PoeStashItem, 0, 2),
			"One Hand Mace":            make([]PoeStashItem, 0, 2),
			"One Hand Sword":           make([]PoeStashItem, 0, 2),
			"Rune Dagger":              make([]PoeStashItem, 0, 2),
			"Sceptre":                  make([]PoeStashItem, 0, 2),
			"Shield":                   make([]PoeStashItem, 0, 1),
			"Staff":                    make([]PoeStashItem, 0, 1),
			"Thrusting One Hand Sword": make([]PoeStashItem, 0, 2),
			"Two Hand Axe":             make([]PoeStashItem, 0, 1),
			"Two Hand Mace":            make([]PoeStashItem, 0, 1),
			"Two Hand Sword":           make([]PoeStashItem, 0, 1),
			"Wand":                     make([]PoeStashItem, 0, 2),
			"Warstaff":                 make([]PoeStashItem, 0, 1),
		},
		MinItemLevel: minItemLevel,
		MaxItemLevel: maxItemlevel,
	}, nil
}

func (s *ItemSet) IsFull() bool {
	return s.armourCount == ArmourCapacity && s.weaponCount == WeaponCapacity
}

func (s *ItemSet) AddStashItem(item PoeStashItem, weaponGreedy bool) error {
	if item.ItemLevel < s.MinItemLevel || (s.MaxItemLevel != -1 && item.ItemLevel > s.MaxItemLevel) {
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
		if _, err := IsClassTwoHandedWeapon(class); err == nil {
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

func getViableRares(tabIndex int, index bleve.Index, searchSize int, restrictMaxLevel bool) (items []PoeStashItem, err error) {
	var filters []string
	if tabIndex != -1 {
		filters = append(filters, fmt.Sprintf("+tabIndex:%d", tabIndex))
	}
	filters = append(filters, fmt.Sprintf("+frameType:%s", poeFrameTypes[PoeFrameTypeRare]))
	filters = append(filters, "+identified:0")
	filters = append(filters, fmt.Sprintf("itemLevel:>=%d", ChaosRecipeMinItemLevel))
	if restrictMaxLevel {
		filters = append(filters, fmt.Sprintf("itemLevel:<=%d", ChaosRecipeMaxItemLevel))
	}
	querystring := strings.Join(filters, " ")
	results, err := QueryIndex(querystring, index, searchSize)
	if err != nil {
		return results, err
	}
	for _, item := range results {
		if item.ItemLevel < ChaosRecipeMinItemLevel || (restrictMaxLevel && item.ItemLevel > ChaosRecipeMaxItemLevel) {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func (c *UnidChaosRecipe) ScanIndex(targetItem *PoeStashItem, tabIndex int, index bleve.Index, findAll bool) (results []RecipeResult, err error) {
	log.Debug("getting viable strict rares (ilvl 60-74)")
	strictRares, err := getViableRares(tabIndex, index, PoeQuadTabSize, true)
	if err != nil {
		return results, err
	}

	log.Debug("getting all viable rares (ilvl 60+)")
	rares, err := getViableRares(tabIndex, index, PoeQuadTabSize, false)
	if err != nil {
		return results, err
	}

	setRares := NewPoeStashItemSet()

	for len(rares) > 1 {
		if len(results) == len(strictRares) {
			log.Debug("reached max number of sets due to valid 60-74 rares")
			break
		}

		var firstItem PoeStashItem
		for firstItemPos := len(results); firstItemPos <= len(strictRares); firstItemPos++ {
			firstItem = strictRares[firstItemPos]
			if !setRares.HasItem(firstItem) {
				firstItem = strictRares[firstItemPos]
				break
			}
		}
		if firstItem.ID == "" {
			log.Debug("ran out of viable 60-74 rares")
			break
		}
		set, err := NewItemSet(ChaosRecipeMinItemLevel, -1)
		if err != nil {
			log.Debug(err)
			break
		}
		ctxLogger := log.WithFields(log.Fields{
			"item": firstItem.ToString(),
			"set":  set.UUID.String(),
		})
		if err := set.AddStashItem(firstItem, true); err != nil {
			ctxLogger.Debug(err)
			strictRares = append(strictRares[:0], strictRares[1:]...)
		} else {
			ctxLogger.Debug("added item (strict ilvl) to chaos recipe")
		}

		if targetItem != nil {
			err = set.AddStashItem(*targetItem, true)
			if err != nil {
				return results, errors.New("invalid target item")
			}
		}
		nextRares := rares[:0]
		for index, stashItem := range rares {
			ctxLogger := log.WithFields(log.Fields{
				"item": stashItem.ToString(),
				"set":  set.UUID.String(),
			})
			if setRares.HasItem(stashItem) {
				ctxLogger.Debug("skipping item, already in another set")
				continue
			}
			err = set.AddStashItem(stashItem, true)
			if err != nil {
				ctxLogger.Debug(err)
				nextRares = append(nextRares, stashItem)
				continue
			} else {
				ctxLogger.Debug("added item to chaos recipe")
			}
			if set.IsFull() {
				nextRares = append(nextRares, rares[index+1:]...)
				break
			}
		}
		if !set.IsFull() {
			log.Debug("failed to find a full set")
			break
		}
		rares = nextRares
		for _, setItem := range set.Items() {
			ctxLogger := log.WithFields(log.Fields{
				"item": setItem.ToString(),
				"set":  set.UUID.String(),
			})
			ctxLogger.Debug("marking item as existing in a set")
			setRares.AddItem(setItem)
		}
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
