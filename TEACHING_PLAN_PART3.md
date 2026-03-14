## 第三讲：核心算法实现（60 分钟）

### 3.1 记忆摄入算法（Ingest Algorithm）

**教授**：欢迎回来！现在我们要深入最核心的部分——算法实现。让我们从记忆摄入开始。

记忆摄入的本质是：**将非结构化文本转换为结构化数据**。这个过程涉及三个关键步骤：

```
原始文本 → LLM 处理 → 结构化数据 → 数据库存储
```

#### 步骤 1：Prompt 工程（Prompt Engineering）

**教授**：LLM 的输出质量完全取决于 prompt 的设计。让我展示一个精心设计的 prompt：

```python
system_prompt = """
You are a Memory Ingest Agent. Analyze the input and respond with ONLY a JSON object:
{
  "summary": "1-2 sentence summary",
  "entities": ["entity1", "entity2"],
  "topics": ["topic1", "topic2"],
  "importance": 0.7
}

Guidelines:
- summary: concise 1-2 sentence summary
- entities: key people, companies, products, concepts (list of strings)
- topics: 2-4 topic tags (list of strings)
- importance: float 0.0-1.0 (how important is this information)

Respond with ONLY the JSON, no other text.
"""
```

**学生**：教授，为什么要强调"ONLY the JSON"？

**教授**：非常好的问题！这涉及到 LLM 的一个特性：**它喜欢"聊天"**。如果不明确限制，LLM 可能返回：

```
Sure! I'd be happy to help analyze this text. Here's the structured information:

```json
{
  "summary": "...",
  "entities": [...]
}
```

As you can see, this text discusses...
```

这样的输出很难解析！所以我们要明确告诉它："只返回 JSON，不要废话"。

**学生**：那如果 LLM 还是返回了额外的文本怎么办？

**教授**：好问题！这就需要**鲁棒的解析策略**。

#### 步骤 2：JSON 解析策略（Robust Parsing）

**教授**：我们需要处理三种情况：

**情况 1：理想情况（纯 JSON）**
```
{"summary": "...", "entities": [...]}
```
→ 直接 `json.loads()` 或 `json.Unmarshal()`

**情况 2：Markdown 代码块**
```
```json
{"summary": "...", "entities": [...]}
```
```
→ 正则提取代码块内容

**情况 3：混合文本**
```
Here's the analysis:
{"summary": "...", "entities": [...]}
Hope this helps!
```
→ 正则查找 JSON 对象

**Python 实现**：
```python
def _extract_json(text: str) -> dict | None:
    # 策略 1：尝试代码块
    m = re.search(r'```(?:json)?\s*\n?(.*?)\n?```', text, re.DOTALL)
    if m:
        try:
            return json.loads(m.group(1).strip())
        except json.JSONDecodeError:
            pass

    # 策略 2：尝试整个文本
    try:
        return json.loads(text.strip())
    except json.JSONDecodeError:
        pass

    # 策略 3：查找任意 JSON 对象
    m = re.search(r'\{.*\}', text, re.DOTALL)
    if m:
        try:
            return json.loads(m.group(0))
        except json.JSONDecodeError:
            pass

    return None
```

**学生**：教授，`re.DOTALL` 是什么意思？

**教授**：好问题！这是一个正则表达式标志。让我解释：

```python
# 默认情况：. 不匹配换行符
text = "line1\nline2"
re.search(r'line1.*line2', text)  # 匹配失败！

# 使用 DOTALL：. 匹配包括换行符在内的所有字符
re.search(r'line1.*line2', text, re.DOTALL)  # 匹配成功！
```

这对于匹配跨行的 JSON 非常重要。

**Go 实现的关键差异**：
```go
// Go 的正则语法不同
re := regexp.MustCompile("(?s)```(?:json)?\\s*\\n?(.*?)\\n?```")
//                        ^^^
//                        (?s) 是 Go 的多行模式标志
```

**学生**：原来如此！那为什么 Go 版本一开始没有 `(?s)` 导致解析失败？

**教授**：完全正确！这是一个真实的 bug。初始版本的正则是：
```go
re := regexp.MustCompile("```(?:json)?\\s*\\n?(.*?)\\n?```")
//                        缺少 (?s)
```

这导致 `.*?` 无法匹配换行符，所以跨行的 JSON 无法提取。修复后：
```go
re := regexp.MustCompile("(?s)```(?:json)?\\s*\\n?(.*?)\\n?```")
//                        ^^^^ 添加多行模式
```

这个案例说明：**细节决定成败**！

#### 步骤 3：重要度评估（Importance Scoring）

**教授**：`importance` 字段是一个 0.0-1.0 的浮点数，表示记忆的重要程度。LLM 如何判断重要度？

**评估维度**：
1. **信息密度**：包含多少关键信息
2. **时效性**：是否涉及截止日期、紧急事项
3. **影响范围**：影响多少人/项目
4. **决策相关**：是否涉及重要决策

**示例**：
```
输入 1: "今天天气不错"
→ importance: 0.1（日常闲聊，不重要）

输入 2: "Q1 目标：降低推理成本 40%"
→ importance: 0.9（关键目标，非常重要）

输入 3: "会议改到下周三下午 3 点"
→ importance: 0.6（中等重要，涉及时间安排）
```

**学生**：这个评分是主观的吗？不同 LLM 会给出不同分数吗？

**教授**：非常好的问题！确实存在**主观性**和**模型差异**。但在实践中：
1. **相对排序**比绝对分数更重要
2. **一致性**比精确度更重要
3. 可以通过 **few-shot learning** 校准

例如，在 prompt 中添加示例：
```
Examples:
- "Buy milk" → importance: 0.2
- "Project deadline: March 31" → importance: 0.8
- "Company acquired by Google" → importance: 1.0
```

### 3.2 记忆整合算法（Consolidation Algorithm）

**教授**：现在我们来看最核心的创新——记忆整合。这个算法模拟人脑在睡眠时的记忆巩固过程。

#### 整合触发条件

**教授**：整合不是随时进行的，需要满足条件：

```python
def should_consolidate():
    # 条件 1：有足够的未整合记忆
    unconsolidated_count = db.execute(
        "SELECT COUNT(*) FROM memories WHERE consolidated = 0"
    ).fetchone()[0]

    if unconsolidated_count < MIN_MEMORIES:  # 默认 2
        return False

    # 条件 2：距离上次整合已经过了足够时间
    last_consolidation = db.execute(
        "SELECT MAX(created_at) FROM consolidations"
    ).fetchone()[0]

    if last_consolidation:
        elapsed = now() - last_consolidation
        if elapsed < CONSOLIDATE_INTERVAL:  # 默认 30 分钟
            return False

    return True
```

**学生**：为什么要等 30 分钟？不能立即整合吗？

**教授**：好问题！这涉及到**效率和质量的权衡**：

**立即整合的问题**：
- 每次摄入都整合 → LLM 调用频繁 → 成本高
- 只有 2-3 条记忆 → 关联不明显 → 洞见质量低

**延迟整合的优势**：
- 积累更多记忆 → 发现更多关联
- 批量处理 → 降低 API 调用次数
- 后台运行 → 不影响用户体验

**类比**：就像写论文，不是写一句就整理一次，而是写完一章再整理。

#### 整合算法流程

**教授**：整合算法分为四个阶段：

```
阶段 1: 检索未整合记忆
    ↓
阶段 2: 构造整合 Prompt
    ↓
阶段 3: LLM 分析生成洞见
    ↓
阶段 4: 更新数据库（连接 + 标记）
```

**阶段 1：检索未整合记忆**

```python
def get_unconsolidated_memories(limit=100):
    rows = db.execute("""
        SELECT id, summary, entities, topics, importance, created_at
        FROM memories
        WHERE consolidated = 0
        ORDER BY created_at DESC
        LIMIT ?
    """, (limit,)).fetchall()

    return [
        {
            "id": r["id"],
            "summary": r["summary"],
            "entities": json.loads(r["entities"]),
            "topics": json.loads(r["topics"]),
            "importance": r["importance"],
        }
        for r in rows
    ]
```

**学生**：为什么要限制 100 条？

**教授**：这涉及到 **LLM 的上下文窗口限制**。即使是大模型，也有 token 限制：

```
Claude Sonnet: ~200K tokens
GPT-4: ~128K tokens
```

100 条记忆大约占用：
```
100 条 × 平均 200 tokens/条 = 20K tokens
```

留出足够空间给 prompt 和响应。

**阶段 2：构造整合 Prompt**

```python
def build_consolidation_prompt(memories):
    # 格式化记忆列表
    memories_text = "\n".join([
        f"- Memory #{m['id']}: {m['summary']} "
        f"(entities: {m['entities']}, topics: {m['topics']})"
        for m in memories
    ])

    system_prompt = """
You are a Memory Consolidation Agent. Analyze the memories below and respond with ONLY a JSON object:
{
  "source_ids": [1, 2, 3],
  "summary": "synthesized summary across all memories",
  "insight": "key pattern or insight discovered",
  "connections": [
    {"from_id": 1, "to_id": 2, "relationship": "how they relate"}
  ]
}

Find connections and patterns across the memories.
Respond with ONLY the JSON, no other text.
"""

    user_prompt = f"Unconsolidated memories:\n{memories_text}"

    return system_prompt, user_prompt
```

**学生**：教授，`connections` 字段的 `relationship` 应该写什么？

**教授**：非常好的问题！`relationship` 应该描述**因果关系**或**逻辑关系**。例如：

```json
{
  "from_id": 1,
  "to_id": 2,
  "relationship": "Reliability improvements can reduce costs by minimizing retries"
}
```

**好的 relationship**：
- 描述具体关系（不是"相关"这种模糊词）
- 说明因果或逻辑（为什么关联）
- 简洁明了（一句话）

**阶段 3：LLM 分析**

```python
async def consolidate_memories(memories):
    system, user = build_consolidation_prompt(memories)

    # 调用 LLM
    response = await llm_client.chat.completions.create(
        model="claude-sonnet-4",
        messages=[
            {"role": "system", "content": system},
            {"role": "user", "content": user}
        ],
        max_tokens=4096
    )

    # 解析响应
    result = extract_json(response.choices[0].message.content)

    return result
```

**阶段 4：更新数据库**

```python
def store_consolidation(result):
    # 1. 存储整合记录
    db.execute("""
        INSERT INTO consolidations (source_ids, summary, insight, created_at)
        VALUES (?, ?, ?, ?)
    """, (
        json.dumps(result["source_ids"]),
        result["summary"],
        result["insight"],
        datetime.now().isoformat()
    ))

    # 2. 更新记忆连接
    for conn in result["connections"]:
        from_id = conn["from_id"]
        to_id = conn["to_id"]
        relationship = conn["relationship"]

        # 双向更新
        for memory_id, linked_id in [(from_id, to_id), (to_id, from_id)]:
            # 读取现有连接
            row = db.execute(
                "SELECT connections FROM memories WHERE id = ?",
                (memory_id,)
            ).fetchone()

            existing = json.loads(row["connections"])

            # 添加新连接
            existing.append({
                "linked_to": linked_id,
                "relationship": relationship
            })

            # 更新数据库
            db.execute(
                "UPDATE memories SET connections = ? WHERE id = ?",
                (json.dumps(existing), memory_id)
            )

    # 3. 标记为已整合
    db.execute(
        f"UPDATE memories SET consolidated = 1 WHERE id IN ({','.join('?' * len(result['source_ids']))})",
        result["source_ids"]
    )

    db.commit()
```

**学生**：教授，为什么要双向更新连接？

**教授**：好问题！这是**图数据结构**的设计。让我画个图：

```
记忆 #1 ←──────→ 记忆 #2
  ↓                 ↓
connections:     connections:
[{linked_to: 2}] [{linked_to: 1}]
```

双向连接的优势：
1. **查询效率**：从任一节点都能找到关联
2. **数据一致性**：保证关系是对称的
3. **图遍历**：支持双向遍历

### 3.3 记忆查询算法（Query Algorithm）

**教授**：查询算法看似简单，实则精妙。它需要：
1. 检索相关记忆
2. 构造上下文
3. 生成答案

#### 检索策略

**教授**：当前实现使用**时间排序**：

```python
def retrieve_memories(limit=50):
    return db.execute("""
        SELECT * FROM memories
        ORDER BY created_at DESC
        LIMIT ?
    """, (limit,)).fetchall()
```

**学生**：这样不是很低效吗？如果有 1000 条记忆，只取最近 50 条，可能错过重要信息。

**教授**：完全正确！这是当前实现的**局限性**。更好的策略是：

**策略 1：重要度排序**
```sql
SELECT * FROM memories
ORDER BY importance DESC, created_at DESC
LIMIT 50
```

**策略 2：主题过滤**
```python
# 提取问题中的关键词
keywords = extract_keywords(question)

# 查询包含这些关键词的记忆
memories = db.execute("""
    SELECT * FROM memories
    WHERE topics LIKE ? OR entities LIKE ?
    ORDER BY importance DESC
    LIMIT 50
""", (f"%{keyword}%", f"%{keyword}%"))
```

**策略 3：向量检索（最优）**
```python
# 1. 将问题转换为向量
question_embedding = embedding_model.encode(question)

# 2. 计算与所有记忆的相似度
similarities = cosine_similarity(
    question_embedding,
    memory_embeddings
)

# 3. 返回最相似的 top-k
top_k_indices = np.argsort(similarities)[-50:]
```

**学生**：向量检索听起来很强大！为什么当前版本没有实现？

**教授**：好问题！这涉及到**工程权衡**：

**向量检索的优势**：
- 语义相似度匹配（理解意思，不只是关键词）
- 检索精度高

**向量检索的代价**：
- 需要额外的向量数据库（Qdrant, Milvus）
- 需要 embedding 模型
- 增加系统复杂度

**当前实现的选择**：
- 简单直接（只需 SQLite）
- 对于小规模数据（<1000 条）足够用
- 易于理解和维护

这是**MVP（最小可行产品）**的思想：先实现核心功能，再逐步优化。

#### 上下文构造

**教授**：查询的关键是构造好的上下文。让我展示：

```python
def build_query_context(question, memories, consolidations):
    # 1. 格式化记忆
    memories_text = "\n".join([
        f"- [Memory {m['id']}] {m['summary']} "
        f"(entities: {m['entities']}, topics: {m['topics']}, "
        f"importance: {m['importance']})"
        for m in memories
    ])

    # 2. 格式化整合历史
    consolidations_text = ""
    if consolidations:
        consolidations_text = "\n\nConsolidation insights:\n" + "\n".join([
            f"- {c['insight']}"
            for c in consolidations
        ])

    # 3. 构造完整上下文
    context = f"""Stored memories:
{memories_text}
{consolidations_text}

Question: {question}"""

    return context
```

**学生**：为什么要包含整合历史？

**教授**：非常好的问题！整合历史包含**跨记忆的洞见**，这是单条记忆没有的。例如：

**只有记忆**：
```
Memory 1: AI agents 增长快
Memory 2: 降低成本 40%
```
→ LLM 只能分别描述这两条

**加上整合历史**：
```
Memory 1: AI agents 增长快
Memory 2: 降低成本 40%
Insight: 可靠性和成本是相互关联的
```
→ LLM 能理解它们的关系

这就是**整合的价值**！

---

### 3.4 第三讲小结

**教授**：让我总结第三讲的核心要点：

**核心算法**：
1. **摄入**：Prompt 工程 + 鲁棒解析 + 重要度评估
2. **整合**：触发条件 + 关联发现 + 双向连接
3. **查询**：检索策略 + 上下文构造 + 答案生成

**关键技术**：
- **正则表达式**：处理 LLM 的不确定输出
- **JSON 解析**：多策略 fallback
- **图数据结构**：双向连接表示关系

**工程权衡**：
- 简单 vs 复杂（时间排序 vs 向量检索）
- 成本 vs 质量（立即整合 vs 延迟整合）
- MVP vs 完美（先实现核心，再优化）

**思考题**：
1. 如何设计一个"遗忘"机制，让不重要的记忆逐渐淡化？
2. 如果两条记忆矛盾（例如"项目延期到 4 月"和"项目 3 月完成"），如何处理？
3. 如何评估整合质量？设计一个评分函数。

---

**教授**：好，最后一讲我们将探讨并发模型和性能优化。休息 10 分钟！

