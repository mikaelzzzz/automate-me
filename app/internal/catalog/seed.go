package catalog

// Seed is the launch catalog (PRD §5.2 executable, §5.3 advised, §5.4
// roadmap). Costs are demo-realistic BRL figures; MinutesSavedPerOcc is the
// default the Value Engine uses until the user's own estimate overrides it.
func Seed() []Recipe {
	return []Recipe{
		// --- executable (8, each mapped to a timeline day in PRD §5.2) ---
		{
			ID: "dishwasher", Title: "Buy a dishwasher (agent purchase)",
			Description: "Full AP2 purchase against the merchant agent; delivery lands on your calendar.",
			Class:       ClassExecutable, Capability: CapAP2Purchase,
			Cost:      CostModel{UpfrontCents: 3000_00, MinutesSavedPerOcc: 55},
			Triggers:  []string{"dish", "louça", "louca", "lavar louça"},
			ProductID: "dw-500",
		},
		{
			ID: "commute-audit", Title: "Commute audit",
			Description: "Compare car/transit/bike and departure windows; see what your commute costs per month.",
			Class:       ClassExecutable, Capability: CapMapsRoutes,
			Cost:     CostModel{MinutesSavedPerOcc: 20},
			Triggers: []string{"commute", "trajeto", "trânsito", "transito", "deslocamento"},
		},
		{
			ID: "leave-on-time", Title: "Leave-on-time blocks",
			Description: "Traffic-predicted departure blocks per appointment; suggests video call when the trip isn't worth its cost.",
			Class:       ClassExecutable, Capability: CapMapsRoutes,
			Cost:     CostModel{MinutesSavedPerOcc: 15},
			Triggers: []string{"meeting", "reunião", "reuniao", "compromisso", "atraso"},
		},
		{
			ID: "calendar-batching", Title: "Chore batching",
			Description: "Consolidates scattered recurring chores into weekly calendar blocks.",
			Class:       ClassExecutable, Capability: CapCalendarWrite,
			Cost:     CostModel{MinutesSavedPerOcc: 10},
			Triggers: []string{"contas", "bills", "e-mail", "email", "limpeza", "cleaning", "organizar"},
		},
		{
			ID: "delegation-draft", Title: "Delegation drafts",
			Description: "Ready-to-send message/ad for hiring a cleaner or helper.",
			Class:       ClassExecutable, Capability: CapGmailDraft,
			Cost:     CostModel{MonthlyRunningCents: 400_00, MinutesSavedPerOcc: 240},
			Triggers: []string{"faxina", "cleaner", "diarista", "passar roupa", "ironing"},
		},
		{
			ID: "boleto-pile", Title: "Boleto pile to calendar",
			Description: "Photo of boletos → amounts, due dates and the 47-digit line extracted; payment-eve reminders scheduled.",
			Class:       ClassExecutable, Capability: CapVision,
			Cost:     CostModel{MinutesSavedPerOcc: 30},
			Triggers: []string{"boleto", "conta pra pagar", "fatura", "bill"},
		},
		{
			ID: "school-note", Title: "School note to calendar",
			Description: "Photo of the paper note or WhatsApp print → family calendar events + drafted reply.",
			Class:       ClassExecutable, Capability: CapVision,
			Cost:     CostModel{MinutesSavedPerOcc: 15},
			Triggers: []string{"escola", "school", "bilhete", "reunião de pais"},
		},
		{
			ID: "teams-report", Title: "Team automation report",
			Description: "Team task list + hourly cost → shareable Automation Opportunities Report.",
			Class:       ClassExecutable, Capability: CapReportGen,
			Cost:     CostModel{MinutesSavedPerOcc: 60},
			Triggers: []string{"equipe", "team", "empresa", "funcionário", "manual"},
		},

		// --- advised (payback cards; PRD §5.3) ---
		{
			ID: "robot-vacuum", Title: "Robot vacuum",
			Class: ClassAdvised, Capability: CapAP2Purchase,
			Cost:      CostModel{UpfrontCents: 2000_00, MinutesSavedPerOcc: 30},
			Triggers:  []string{"aspirar", "vacuum", "varrer", "chão", "chao"},
			ProductID: "rv-200",
		},
		{
			ID: "grocery-delivery", Title: "Grocery delivery (agent purchase)",
			// Executable, not advised: the merchant sells this basket and the
			// AP2 rail buys it. A cheap, repeating purchase is exactly what a
			// standing spending authorization is for.
			Class: ClassExecutable, Capability: CapAP2Purchase,
			Cost: CostModel{MonthlyRunningCents: 80_00, MinutesSavedPerOcc: 120},
			// "supermarket" was missing: the matcher is a substring test and
			// none of the other triggers appear inside the English word, so an
			// English-declared grocery run matched nothing.
			Triggers:  []string{"mercado", "grocery", "supermercado", "supermarket", "compras"},
			ProductID: "grocery-basic",
		},
		{
			ID: "laundry-service", Title: "Wash-and-fold service",
			Class:    ClassAdvised,
			Cost:     CostModel{MonthlyRunningCents: 250_00, MinutesSavedPerOcc: 90},
			Triggers: []string{"lavanderia", "laundry", "roupa"},
		},
		{
			ID: "auto-pay", Title: "Auto-pay migration",
			Class:    ClassAdvised,
			Cost:     CostModel{MinutesSavedPerOcc: 25},
			Triggers: []string{"boleto", "conta", "bills", "pagamento"},
		},
		{
			ID: "farmacia-popular", Title: "Farmácia Popular check",
			Description: "Continuous-use meds may be free under the federal program.",
			Class:       ClassAdvised,
			Cost:        CostModel{MinutesSavedPerOcc: 20},
			Triggers:    []string{"remédio", "remedio", "farmácia", "farmacia", "medication"},
		},
		{
			ID: "sne-discount", Title: "SNE 40% fine discount",
			Class:    ClassAdvised,
			Cost:     CostModel{MinutesSavedPerOcc: 10},
			Triggers: []string{"multa", "detran", "trânsito", "transito"},
		},
		{
			ID: "car-worth-it", Title: "Is your car worth it?",
			Description: "Total cost of ownership vs ride-hailing + transit for your real trips.",
			Class:       ClassAdvised, Capability: CapMapsRoutes,
			Cost:     CostModel{MinutesSavedPerOcc: 0},
			Triggers: []string{"carro", "car", "ipva", "seguro", "estacionamento"},
		},
		{
			ID: "virtual-assistant", Title: "Delegate to a virtual assistant",
			Class: ClassAdvised, Capability: CapGmailDraft,
			Cost:     CostModel{MonthlyRunningCents: 1200_00, MinutesSavedPerOcc: 840},
			Triggers: []string{"agenda", "admin", "tarefas repetitivas", "planilha"},
		},
		{
			ID: "forgotten-money", Title: "Forgotten money ritual",
			Description: "Banco Central SVR, Nota Fiscal Paulista credits, PIS — money behind a gov.br login.",
			Class:       ClassAdvised,
			Cost:        CostModel{MinutesSavedPerOcc: 0},
			Triggers:    []string{"dinheiro esquecido", "svr", "nota fiscal"},
		},

		// --- roadmap (PRD §5.4; shown for vision, never proposed) ---
		{ID: "open-finance", Title: "Open Finance monitor", Class: ClassRoadmap, Triggers: []string{"assinatura", "tarifa"}},
		{ID: "whatsapp-channel", Title: "WhatsApp agent", Class: ClassRoadmap, Triggers: []string{"whatsapp"}},
		{ID: "nfse", Title: "Automatic NFS-e", Class: ClassRoadmap, Triggers: []string{"nota fiscal de serviço", "nfse"}},
		{ID: "health-reimburse", Title: "Health-plan reimbursement", Class: ClassRoadmap, Triggers: []string{"reembolso", "plano de saúde"}},
		{ID: "inbox-agent", Title: "Self-answering inbox", Class: ClassRoadmap, Triggers: []string{"inbox", "caixa de entrada"}},
		{ID: "gas-prediction", Title: "Gas canister prediction", Class: ClassRoadmap, Triggers: []string{"gás", "gas", "botijão", "botijao"}},
		{ID: "pantry-menu", Title: "Weekly menu from pantry photo", Class: ClassRoadmap, Triggers: []string{"cardápio", "cozinhar", "janta"}},
		{ID: "no-show", Title: "Anti no-show confirmations", Class: ClassRoadmap, Triggers: []string{"no-show", "cliente faltou", "confirmação"}},
		{ID: "errand-loop", Title: "Optimized errand loop", Class: ClassRoadmap, Triggers: []string{"farmácia", "correio", "banco"}},
	}
}
