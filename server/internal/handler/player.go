package handler

import (
	"google.golang.org/protobuf/proto"

	"astral-party-server/internal/pb/model"
	"astral-party-server/internal/store"
)

// SkipTutorial marks the novice tutorial achievement as done server-side.
// The client runs the tutorial when TaskLogic.GetAchieveInfo_1(1) == 0, i.e.
// Player.Task.Condition[1] is missing/zero; setting it to 1 skips the whole
// flow. Wired from the -skip-tutorial flag via handler.Deps.
var SkipTutorial bool

// tutorialAchievementType is the achievement id the client's
// TutorialLogic.GetTutorialStatus checks.
const tutorialAchievementType = 1

func applyTutorialSkip(t *model.TaskInfo) {
	if !SkipTutorial || t == nil {
		return
	}
	for _, e := range t.Condition {
		if e.Key == tutorialAchievementType {
			return
		}
	}
	t.Condition = append(t.Condition, &model.TaskInfo_ConditionEntry{
		Key:   tutorialAchievementType,
		Value: 1,
	})
}

// applyMaxOut tops every account up to maxed resources: all heroes owned,
// every config item at 99999 in the bag, and the Star Disc / Golden Star
// Disc currencies at 9999999. Applied on default players and on every
// persisted load, so existing accounts are topped up on login.
func applyMaxOut(p *model.Player) {
	if p == nil {
		return
	}
	byId := make(map[int32]*model.ItemEtc, len(p.BagItems))
	for _, b := range p.BagItems {
		byId[b.ItemId] = b
	}
	for _, def := range allItems {
		count := maxItemCount
		if def.Id == starDiscItemId || def.Id == goldenStarDiscItemId {
			count = maxCurrencyCount
		}
		if existing, ok := byId[def.Id]; ok {
			if existing.Count < count {
				existing.Count = count
			}
			continue
		}
		bagItem := &model.ItemEtc{ItemId: def.Id, Count: count}
		p.BagItems = append(p.BagItems, bagItem)
		byId[def.Id] = bagItem
	}
	owned := make(map[int32]bool, len(p.RoleCard))
	for _, e := range p.RoleCard {
		owned[e.Key] = true
	}
	for _, heroId := range allHeroIds {
		if owned[heroId] {
			continue
		}
		p.RoleCard = append(p.RoleCard, &model.Player_RoleCardEntry{
			Key: heroId,
			Value: &model.RoleCard{
				DefId:          heroId,
				Collected:      true,
				IsBreakThrough: true,
			},
		})
	}
}

// NewDefaultPlayer builds the full Player payload the client expects inside
// ConnectS2C. GameLogicManager.InitAccount fans the Player out to every
// subsystem (bag, shop, task, mail, friends, battle pass, ...). Nested
// message fields must be non-nil: TaskLogic.InitFromServer dereferences
// playerTask directly, and other subsystems follow the same pattern.
func NewDefaultPlayer(row *store.PlayerRow) *model.Player {
	p := &model.Player{
		Id:         row.ID,
		Nick:       row.Name,
		Level:      row.Level,
		Exp:        row.Exp,
		CreateTime: row.CreatedAt,
		SaveTime:   row.CreatedAt,
		Hero:           &model.Hero{},
		ShopInfo:       &model.PlayerShopInfo{},
		ShowPlayer:     &model.ShowPlayerInfo{},
		Task:           &model.TaskInfo{},
		ClientData:     &model.ClientData{},
		Friends:        &model.FriendList{},
		BattlePass:     &model.BattlePass{},
		PayAmountInfo:  &model.PayAmountInfo{},
		InviteInfo:     &model.InviteInfo{},
		MissionMod:     &model.MissionMod{},
		SingleInfo:     &model.SingleInfo{},
		CreditInfo:     &model.CreditInfo{},
		SportsMeetInfo: &model.SportsMeetInfo{},
		ReturnInfo:     &model.ReturnInfo{},
		TagInfo:        &model.PlayerTagInfo{},
		GuildInfo:      &model.PlayerGuildInfo{},
		UsePlan:        1,
		FashionPlan: []*model.FashionPlan{{
			Plan: 1,
			// Real item ids: id 0 has no ItemInfoConfigure entry and
			// BottomMenuPanel.Refresh dereferences the lookup result
			// unguarded (NRE on home). 70108/71208 are the headshot and
			// label/background the tutorial scene itself uses. Keys 3-6
			// (cardback/dice/effect/KV) have client-side fallbacks for 0.
			Fashion: []*model.FashionPlan_FashionEntry{
				{Key: 1, Value: 70108},
				{Key: 2, Value: 71208},
				{Key: 3, Value: 0},
				{Key: 4, Value: 0},
				{Key: 5, Value: 0},
				{Key: 6, Value: 0},
			},
		}},
	}
	applyTutorialSkip(p.Task)
	applyMaxOut(p)
	return p
}

func marshalPlayer(p *model.Player) ([]byte, error) {
	return proto.Marshal(p)
}

func unmarshalPlayer(blob []byte) (*model.Player, error) {
	p := &model.Player{}
	if err := proto.Unmarshal(blob, p); err != nil {
		return nil, err
	}
	fillEmptyPlayerMessages(p)
	return p, nil
}

// fillEmptyPlayerMessages guarantees nested message fields are non-nil even
// for rows persisted before the field existed.
func fillEmptyPlayerMessages(p *model.Player) {
	if p.Hero == nil {
		p.Hero = &model.Hero{}
	}
	if p.ShopInfo == nil {
		p.ShopInfo = &model.PlayerShopInfo{}
	}
	if p.ShowPlayer == nil {
		p.ShowPlayer = &model.ShowPlayerInfo{}
	}
	if p.Task == nil {
		p.Task = &model.TaskInfo{}
	}
	if p.ClientData == nil {
		p.ClientData = &model.ClientData{}
	}
	if p.Friends == nil {
		p.Friends = &model.FriendList{}
	}
	if p.BattlePass == nil {
		p.BattlePass = &model.BattlePass{}
	}
	if p.PayAmountInfo == nil {
		p.PayAmountInfo = &model.PayAmountInfo{}
	}
	if p.InviteInfo == nil {
		p.InviteInfo = &model.InviteInfo{}
	}
	if p.MissionMod == nil {
		p.MissionMod = &model.MissionMod{}
	}
	if p.SingleInfo == nil {
		p.SingleInfo = &model.SingleInfo{}
	}
	if p.CreditInfo == nil {
		p.CreditInfo = &model.CreditInfo{}
	}
	if p.SportsMeetInfo == nil {
		p.SportsMeetInfo = &model.SportsMeetInfo{}
	}
	if p.ReturnInfo == nil {
		p.ReturnInfo = &model.ReturnInfo{}
	}
	if p.TagInfo == nil {
		p.TagInfo = &model.PlayerTagInfo{}
	}
	if p.GuildInfo == nil {
		p.GuildInfo = &model.PlayerGuildInfo{}
	}
	if p.FashionPlan == nil {
		p.FashionPlan = []*model.FashionPlan{{
			Plan: 1,
			Fashion: []*model.FashionPlan_FashionEntry{
				{Key: 1, Value: 0},
				{Key: 2, Value: 0},
				{Key: 3, Value: 0},
				{Key: 4, Value: 0},
				{Key: 5, Value: 0},
				{Key: 6, Value: 0},
			},
		}}
	}
	if p.UsePlan == 0 {
		p.UsePlan = 1
	}
	for _, plan := range p.FashionPlan {
		for _, e := range plan.Fashion {
			if (e.Key == 1 || e.Key == 2) && e.Value == 0 {
				if e.Key == 1 {
					e.Value = 70108
				} else {
					e.Value = 71208
				}
			}
		}
	}
	applyTutorialSkip(p.Task)
	applyMaxOut(p)
}
