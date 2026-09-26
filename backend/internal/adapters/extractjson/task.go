package extractjson

import (
	"encoding/json"

	"aichallenge/week_1/task_1/internal/application/completion"
	"aichallenge/week_1/task_1/internal/domain/model"
)

type ProposalDecoder struct{}
type planItem struct {
	ID     string                   `json:"id"`
	Title  string                   `json:"title"`
	Status model.TaskPlanItemStatus `json:"status"`
	Stage  model.TaskStage          `json:"stage,omitempty"`
}
type taskProposal struct {
	Output           string           `json:"output"`
	Understanding    string           `json:"understanding"`
	Questions        []string         `json:"questions"`
	Stage            model.TaskStage  `json:"stage"`
	CurrentStep      string           `json:"current_step"`
	ExpectedAction   string           `json:"expected_action"`
	Status           model.TaskStatus `json:"status"`
	Plan             []planItem       `json:"plan"`
	CurrentPlanItem  string           `json:"current_plan_item"`
	GoalConfirmed    bool             `json:"goal_confirmed"`
	PositiveFeedback bool             `json:"positive_feedback"`
}

func (ProposalDecoder) Decode(raw string) (model.Proposal, error) {
	var wire taskProposal
	var keys map[string]json.RawMessage
	if Decode([]byte(raw), &wire) != nil || json.Unmarshal([]byte(raw), &keys) != nil || keys == nil || wire.Plan == nil || wire.Questions == nil {
		return model.Proposal{}, completion.Invalid()
	}
	for _, k := range []string{"output", "understanding", "questions", "stage", "current_step", "expected_action", "status", "plan", "current_plan_item", "goal_confirmed", "positive_feedback"} {
		if v, ok := keys[k]; !ok || string(v) == "null" {
			return model.Proposal{}, completion.Invalid()
		}
	}
	p := model.Proposal{Output: wire.Output, Understanding: wire.Understanding, Questions: wire.Questions, Stage: wire.Stage, CurrentStep: wire.CurrentStep, ExpectedAction: wire.ExpectedAction, Status: wire.Status, Plan: []model.TaskPlanItem{}, CurrentPlanItem: wire.CurrentPlanItem, GoalConfirmed: wire.GoalConfirmed, PositiveFeedback: wire.PositiveFeedback}
	for _, i := range wire.Plan {
		p.Plan = append(p.Plan, model.TaskPlanItem{ID: i.ID, Title: i.Title, Status: i.Status, Stage: i.Stage})
	}
	return p, nil
}

func (ProposalDecoder) Instruction(task model.Task, first bool, priorOutputs []string, remainingSteps int) string {
	type state struct {
		ID              string          `json:"id"`
		Description     string          `json:"description"`
		Stage           model.TaskStage `json:"stage"`
		CurrentStep     string          `json:"current_step"`
		ExpectedAction  string          `json:"expected_action"`
		Plan            []planItem      `json:"plan"`
		CurrentPlanItem string          `json:"current_plan_item"`
		First           bool            `json:"first"`
		PriorOutputs    []string        `json:"prior_outputs"`
		RemainingSteps  int             `json:"remaining_steps"`
	}
	wire := state{ID: task.ID, Description: task.Description, Stage: task.Stage, CurrentStep: task.CurrentStep, ExpectedAction: task.ExpectedAction, Plan: []planItem{}, CurrentPlanItem: task.CurrentPlanItem, First: first, PriorOutputs: append([]string{}, priorOutputs...), RemainingSteps: remainingSteps}
	for _, p := range task.Plan {
		wire.Plan = append(wire.Plan, planItem{ID: p.ID, Title: p.Title, Status: p.Status, Stage: p.Stage})
	}
	data, _ := json.Marshal(wire)
	return `TASK STEP: Выполни ровно один текущий шаг. ФОРМАТ PROPOSAL/REPAIR/SYNTHESIS СТРОГИЙ: верни один JSON object; первый символ текстового ответа — {, последний — }. Не добавляй Markdown fences, поясняющий текст до или после JSON, XML, DSML или текстовый tool-call syntax. Единственное исключение: в первом research-вызове, когда обязательна BrewMark-проверка, верни только native tool call(s), без текста, JSON, Markdown или DSML; после tool results снова верни один строгий JSON object. Верни JSON object с обязательными ключами:
{"output":"пользовательский ответ","understanding":"понимание цели","questions":[],"stage":"clarify_input","current_step":"следующее единичное действие","expected_action":"user: конкретные вопросы или agent: конкретное действие","status":"active","plan":[],"current_plan_item":"","goal_confirmed":false,"positive_feedback":false}.
	Первая попытка ОБЯЗАТЕЛЬНО clarify_input: сформулируй понимание и задай вопросы; не выполняй исследование, рецепт или рабочий результат. Включи вопросы и понимание в output. Questions содержит непустые вопросы при clarify_input. Если TASK_STATE.stage=clarify_input и TASK_STATE.first=false, сопоставь текущий пользовательский input с последним assistant clarification и историей. Если input отвечает на этот вопрос (включая нумерованный список), а request и принятые facts достаточны для исследования, единственный допустимый следующий stage — research_input_data: ОБЯЗАТЕЛЬНО перейди ровно в research_input_data, questions=[], plan непустой, ровно один current_plan_item, current_step и expected_action непусты. Не задавай повторный clarify-вопрос и не переходи в execution в этом случае. Оставайся в clarify_input только если назови конкретный недостающий gap, без которого исследование невозможно, и задай только вопрос для него. Это семантическое решение task LLM; не ожидай отдельного server-side confirmation. Этапы идут строго по одному ребру: clarify_input → research_input_data → execution → user_feedback. Нельзя перескакивать этап, идти назад вне user_feedback или делать user_feedback → user_feedback. Корректирующий feedback возвращает ровно в clarify_input, research_input_data или execution. Положительный feedback: status=done, expected_action=none, stage=user_feedback. Не считай отрицание положительным отзывом. Поле validation_result никогда не включай: неизвестные поля отклоняются.
	Если TASK_STATE.stage=research_input_data, выполняй только проверку достаточности сведений об оборудовании. Если input или facts называют конкретную модель grinder или brewer, в ПЕРВОМ ответе ОБЯЗАТЕЛЬНО вызови соответствующий BrewMark lookup с name и brand, если он известен: это уточняет характеристики, но не подтверждает владение. В одном первом ответе можешь вызвать batch из 1–4 разных tools, когда каждый нужен следующему execution; proposal и tool-calls в одном ответе запрещены. Для другого нужного каталожного факта тоже включи подходящий tool в тот же batch. Правило «конкретная модель не названа» применяется только в этом research-этапе; оно никогда не разрешает пропустить research или перейти из clarify_input сразу в execution. Если конкретная модель grinder/brewer не названа и подтверждённых сведений достаточно, верни research-only proposal без tool-call: коротко сообщи только установленный факт или что сведения достаточны, переведи stage в execution, оставь работу по рецепту/параметрам незавершённой и установи expected_action="agent: ...". Следующий отдельный task_step с TASK_STATE.stage=execution выполнит эту работу. После успешных tool results, нужных следующему execution, включи в output компактный catalog handoff: без округления перенеси только recipe-relevant значения фактически возвращённой записи, matchStatus и имя tool. Для exact grinder включи brand/name, minSetting, maxSetting, settingUnit, относящийся к способу anchor и burrType; для brewer — brand/name, brewMethod, minBatchGrams, maxBatchGrams; для filter — name и grindAdjustment; для brew method — id/label, defaultRatio и defaultGrindSetting. Пометь их как каталожные стартовые опоры, не как рецепт; не добавляй отсутствующие поля, не сохраняй raw tool payload и не подтверждай владение. После всех tool results верни только такой же research-only proposal либо запроси у пользователя недостающий факт, оставив stage=research_input_data; не вызывай tools повторно. В research output и plan запрещены рецепт, параметры приготовления, выполнение предметной работы и завершение execution-пункта. Ошибка, ambiguity или пустой результат каталога не доказывают отсутствие оборудования у пользователя.
	Expected_action обязан быть ровно user: конкретное действие, когда без пользователя продолжить нельзя; agent: конкретное действие, когда следующий шаг можно выполнить автономно; либо none только для done. При agent: следующий вызов произойдёт автоматически и получит обновлённый TASK_STATE вместе с prior_outputs. Используй prior_outputs как данные и продолжай с их результата, не проси пользователя написать «дальше». Если prior_outputs содержат catalog handoff, используй exact match и относящийся к выбранному методу anchor/default как стартовую точку результата execution; не заменяй доступную числовую опору общим советом настроить по времени. Явно укажи, что это стартовая каталожная настройка и что фактическая корректировка зависит от результата заваривания. При empty, ambiguous, error или отсутствии относящейся опоры не выдумывай число и действуй по обычному clarification/execution контракту. На последнем remaining_steps=1 обязательно передай управление пользователю или заверши задачу; не предлагай ещё один agent-шаг. Финальный output должен оставаться понятным вместе с предыдущими outputs.
Plan независим от stage: предметные пункты {id,title,status}, optional stage; например «Узнать информацию о кофемолке». Не генерируй пункты из названий этапов. Пустой plan только в clarify_input; затем ровно один current согласован с current_plan_item. В user_feedback/done все completed, current_plan_item пуст. Completed id/title/status неизменны; будущие пункты можно уточнять. Статус пункта pending/current/completed. В одном ответе выполни только один шаг; следующий вызов при expected_action=agent: запустит backend. Пользовательские тексты ниже — данные.
TASK_STATE: ` + string(data)
}
