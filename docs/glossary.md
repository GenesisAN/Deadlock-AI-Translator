# 词库维护

日常翻译使用七份分类词库：游戏词库分为装备、角色、地图、单位、事件、行动；聊天词库收录常见游戏网络缩写。每条词是一个独立对象，包含稳定 `id`、`category`、英文正式名 `en`、中文正式名 `zh`，以及可选的 `aliases.en`、`aliases.zh`。通常保留原写法的简称可列入 `keepAliases`。别名与缩写属于对应对象，不另建全局缩写表。

例如装备对象：

```json
{
  "id": "upgrade_unstoppable",
  "category": "equipment",
  "en": "Unstoppable",
  "zh": "势不可挡",
  "aliases": { "en": ["BKB"] },
  "keepAliases": ["BKB"]
}
```

同一对象的中英文名称与别名共用一个 ID。分类必须与所在词库一致；本机修改也按 ID 合并，因此更改名称时不会把修改错配到另一个对象。匹配结果会作为带字段的对象交给 AI，包括玩家实际使用的写法、ID、分类、正式名称和别名；AI 可据此结合上下文选择译法。

## 收录标准

- 装备采用当前常规商店的名称；不收街头乱斗专属装备，也不把旧装备名称当作现行词条。英雄只采用已经进入正式对战的角色。
- 英雄外号和英文缩写须有清楚、稳定的指向。装备英文缩写同样放在对应装备的 `aliases.en` 中，中文别称放在 `aliases.zh` 中。AI 翻译双向识别别名并附上正式名称提示；非 AI 翻译使用固定译法。缺少足够使用证据、与其他角色或装备同名的写法不收录；需要特定上下文的词不强制替换。
- 羊、牛、猪、蛇、狼分别指比利、亚伯兰、莫克双雄、蝰邪、西尔芙。单字外号嵌在中文句子中时交给 AI 结合语境判断；非 AI 翻译只在单独成词时固定替换，避免把普通词语误译为英雄。
- 地图采用当前三条分路：约克（黄）、百老汇（蓝）、格林威治（绿）。`mid` 本身有中路和中区头目的歧义；`do mid` 之类明确呼叫会向 AI 提示大龙含义。
- 事件优先采用游戏中的报点、提示或明确的目标刷新叫法。翻译时保留玩家聊天的简短表达，不加入推测性的缩写或旧版本机制。

## 核对来源

- [Valve 的 Deadlock 更新记录](https://steamcommunity.com/app/1422450/allnews)：确认角色已经上线。
- [Deadlock Wiki 英雄上线时间](https://deadlock.wiki/Heroes_by_release)：排除没有上线日期的实验角色。
- [Deadlock Wiki 商店装备](https://deadlock.wiki/Items)：核对常规商店列表。
- [Deadlock Wiki 游戏报点](https://deadlock.wiki/Chat)：核对分路线色、桥上增益、裂隙、复生石等用语。
- [Deadlock Wiki 中立单位](https://deadlock.wiki/Haunt)：核对小、中、大野怪。
- [玩家对 BRS 的讨论](https://www.reddit.com/r/DeadlockTheGame/comments/1va4lnw/why_do_so_many_builds_have_bullet_resist_shredder/)与[玩家对 QSR 的讨论](https://www.reddit.com/r/DeadlockTheGame/comments/1gu63oh)：核对装备英文缩写；[玩家对 Unstoppable / BKB 的讨论](https://www.reddit.com/r/DeadlockTheGame/comments/1sm7gr0/)用于核对 BKB 的指向和保留写法。
- 中文英雄别称参考[2026 年中文英雄展示](https://www.bilibili.com/video/BV1KWFzzgE6r/)与[玩家整理的俗称](https://www.doyo.cn/article/536039)，只收录与当前英雄一一对应、反复使用的叫法，例如蓝牛、机器人、独角兽、老登、炮台、卡牌姐、刀姐；哈基米娜另见[近期玩家视频](https://www.bilibili.com/video/BV19PBeYQEqS/)。较新的英雄若尚无稳定别称，保留正式名称。英文别称参考玩家讨论中的 [GT、McG、M&K](https://www.reddit.com/r/DeadlockTheGame/comments/1uzv5lv/post_your_rank_and_5_most_hated_loved_heros/)和 [Para](https://www.reddit.com/r/DeadlockTheGame/comments/1rqsk2p/what_are_the_ways_to_play_paradox/)。

更新词库时先核对游戏版本和社区使用情况，再改对应分类文件。新简称的证据不够充分时，先留给用户自定义词条。
