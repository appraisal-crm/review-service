package service

import (
	"context"
	"testing"
	"time"

	"github.com/appraisal-crm/review-service/internal/domain"
	"github.com/google/uuid"
)

type formulaMockRepo struct {
	appraisals  map[uuid.UUID]*domain.Appraisal
	comparables map[uuid.UUID][]domain.Comparable
	configs     map[string]*domain.ApartmentFormulaConfig
}

func newFormulaMockRepo() *formulaMockRepo {
	return &formulaMockRepo{
		appraisals:  make(map[uuid.UUID]*domain.Appraisal),
		comparables: make(map[uuid.UUID][]domain.Comparable),
		configs:     make(map[string]*domain.ApartmentFormulaConfig),
	}
}

func (m *formulaMockRepo) Create(ctx context.Context, a *domain.Appraisal) (bool, error) {
	m.appraisals[a.ID] = a
	return true, nil
}

func (m *formulaMockRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Appraisal, error) {
	a, ok := m.appraisals[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	a.Comparables = m.comparables[id]
	return a, nil
}

func (m *formulaMockRepo) GetByRequestID(ctx context.Context, requestID uuid.UUID) (*domain.Appraisal, error) {
	for _, a := range m.appraisals {
		if a.RequestID == requestID {
			return a, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (m *formulaMockRepo) Update(ctx context.Context, a *domain.Appraisal, prev time.Time) error {
	m.appraisals[a.ID] = a
	return nil
}

func (m *formulaMockRepo) Complete(ctx context.Context, id uuid.UUID, completedAt time.Time, ev domain.EventEnvelope) error {
	a, ok := m.appraisals[id]
	if !ok {
		return domain.ErrNotFound
	}
	a.Status = domain.StatusCompleted
	a.CompletedAt = &completedAt
	return nil
}

func (m *formulaMockRepo) AddComparable(ctx context.Context, comp *domain.Comparable) error {
	m.comparables[comp.AppraisalID] = append(m.comparables[comp.AppraisalID], *comp)
	return nil
}

func (m *formulaMockRepo) DeleteComparable(ctx context.Context, appraisalID, comparableID uuid.UUID) error {
	var remaining []domain.Comparable
	for _, c := range m.comparables[appraisalID] {
		if c.ID != comparableID {
			remaining = append(remaining, c)
		}
	}
	m.comparables[appraisalID] = remaining
	return nil
}

func (m *formulaMockRepo) ListByAppraiserID(ctx context.Context, id uuid.UUID) ([]*domain.Appraisal, error) {
	return nil, nil
}

func (m *formulaMockRepo) ListAll(ctx context.Context, limit, offset int) ([]*domain.Appraisal, error) {
	return nil, nil
}

func (m *formulaMockRepo) GetFormulaConfig(ctx context.Context, id string) (*domain.ApartmentFormulaConfig, error) {
	if cfg, ok := m.configs[id]; ok {
		return cfg, nil
	}
	defaultCfg := domain.DefaultApartmentFormulaConfig()
	return &defaultCfg, nil
}

func (m *formulaMockRepo) SaveFormulaConfig(ctx context.Context, cfg *domain.ApartmentFormulaConfig) error {
	m.configs[cfg.ID] = cfg
	return nil
}

func TestCalculateApartment(t *testing.T) {
	repo := newFormulaMockRepo()
	svc := NewAppraisalService(repo, stubStorage{})

	input := domain.ApartmentCalculationInput{
		Subject: domain.ApartmentObjectInput{
			Area:          60.0,
			District:      "Центральный",
			Condition:     domain.ConditionGood,
			RepairClassID: "class_2",
			Floor:         domain.FloorMiddle,
		},
		Analogs: []domain.ApartmentAnalogInput{
			{
				PricePerSqM:       150000,
				BargainingPercent: 5.0,
				Area:              55.0,
				District:          "Центральный",
				Condition:         domain.ConditionGood,
				RepairClassID:     "class_2",
				Floor:             domain.FloorMiddle,
			},
			{
				PricePerSqM:       140000,
				BargainingPercent: 3.0,
				Area:              65.0,
				District:          "Северный",
				Condition:         domain.ConditionSatisfactory,
				RepairClassID:     "class_3",
				Floor:             domain.FloorFirst,
			},
			{
				PricePerSqM:       160000,
				BargainingPercent: 7.0,
				Area:              60.0,
				District:          "Западный",
				Condition:         domain.ConditionExcellent,
				RepairClassID:     "class_1",
				Floor:             domain.FloorLast,
			},
			{
				PricePerSqM:       135000,
				BargainingPercent: 2.0,
				Area:              58.0,
				District:          "Центральный",
				Condition:         domain.ConditionGood,
				RepairClassID:     "class_2",
				Floor:             domain.FloorMiddle,
			},
		},
	}

	result, err := svc.CalculateApartment(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Analogs) != 4 {
		t.Fatalf("expected 4 analogs in result, got %d", len(result.Analogs))
	}

	var sumWeights float64
	for i, a := range result.Analogs {
		if a.PriceAfterBargaining <= 0 {
			t.Errorf("analog %d: expected positive price after bargaining", i+1)
		}
		if a.PriceAfterFloor <= 0 {
			t.Errorf("analog %d: expected positive final adjusted price", i+1)
		}
		if a.Weight <= 0 || a.Weight > 1 {
			t.Errorf("analog %d: invalid weight %f", i+1, a.Weight)
		}
		sumWeights += a.Weight
	}

	// Sum of weights should be approximately 1.0 (with slight rounding)
	if sumWeights < 0.99 || sumWeights > 1.01 {
		t.Errorf("expected sum of weights ~1.0, got %f", sumWeights)
	}

	if result.WeightedPricePerSqM <= 0 {
		t.Errorf("expected positive weighted price per sqm, got %f", result.WeightedPricePerSqM)
	}

	expectedTotal := result.WeightedPricePerSqM * input.Subject.Area
	diff := result.TotalMarketValue - expectedTotal
	if diff < -1 || diff > 1 {
		t.Errorf("total market value mismatch: expected ~%f, got %f", expectedTotal, result.TotalMarketValue)
	}
}

func TestApartmentFormulaAdminUpdate(t *testing.T) {
	repo := newFormulaMockRepo()
	svc := NewAppraisalService(repo, stubStorage{})

	cfg, err := svc.GetApartmentFormulaConfig(context.Background())
	if err != nil {
		t.Fatalf("GetApartmentFormulaConfig failed: %v", err)
	}

	// Update scale formula parameter A and district prices
	cfg.ScaleFormula.A = 1.25
	cfg.DistrictPrices = append(cfg.DistrictPrices, domain.DistrictPrice{Name: "Новый Район", Price: 150000})

	updated, err := svc.UpdateApartmentFormulaConfig(context.Background(), cfg, "admin-user-1")
	if err != nil {
		t.Fatalf("UpdateApartmentFormulaConfig failed: %v", err)
	}

	if updated.ScaleFormula.A != 1.25 {
		t.Errorf("expected updated scale formula A=1.25, got %f", updated.ScaleFormula.A)
	}

	// Verify persistence in repo
	reloaded, err := svc.GetApartmentFormulaConfig(context.Background())
	if err != nil {
		t.Fatalf("reloaded config failed: %v", err)
	}
	if reloaded.ScaleFormula.A != 1.25 {
		t.Errorf("expected reloaded scale formula A=1.25, got %f", reloaded.ScaleFormula.A)
	}

	// Test Reset to defaults
	resetCfg, err := svc.ResetApartmentFormulaConfig(context.Background(), "admin-user-1")
	if err != nil {
		t.Fatalf("ResetApartmentFormulaConfig failed: %v", err)
	}
	if resetCfg.ScaleFormula.A != 1.17 {
		t.Errorf("expected reset scale formula A=1.17, got %f", resetCfg.ScaleFormula.A)
	}
}

func TestApplyApartmentCalculation(t *testing.T) {
	repo := newFormulaMockRepo()
	svc := NewAppraisalService(repo, stubStorage{})

	appraisalID := uuid.New()
	reqID := uuid.New()
	now := time.Now()
	repo.appraisals[appraisalID] = &domain.Appraisal{
		ID:        appraisalID,
		RequestID: reqID,
		Status:    domain.StatusInProgress,
		CreatedAt: now,
		UpdatedAt: now,
	}

	input := domain.ApartmentCalculationInput{
		Subject: domain.ApartmentObjectInput{
			Area:          50.0,
			District:      "Центральный",
			Condition:     domain.ConditionGood,
			RepairClassID: "class_2",
			Floor:         domain.FloorMiddle,
		},
		Analogs: []domain.ApartmentAnalogInput{
			{PricePerSqM: 100000, BargainingPercent: 2.0, Area: 50.0, District: "Центральный", Condition: domain.ConditionGood, RepairClassID: "class_2", Floor: domain.FloorMiddle},
			{PricePerSqM: 100000, BargainingPercent: 2.0, Area: 50.0, District: "Центральный", Condition: domain.ConditionGood, RepairClassID: "class_2", Floor: domain.FloorMiddle},
			{PricePerSqM: 100000, BargainingPercent: 2.0, Area: 50.0, District: "Центральный", Condition: domain.ConditionGood, RepairClassID: "class_2", Floor: domain.FloorMiddle},
			{PricePerSqM: 100000, BargainingPercent: 2.0, Area: 50.0, District: "Центральный", Condition: domain.ConditionGood, RepairClassID: "class_2", Floor: domain.FloorMiddle},
		},
	}

	updated, err := svc.ApplyApartmentCalculation(context.Background(), appraisalID, input)
	if err != nil {
		t.Fatalf("ApplyApartmentCalculation failed: %v", err)
	}

	if updated.MarketValue == nil || *updated.MarketValue == "" {
		t.Fatal("expected market_value to be updated")
	}

	if len(updated.CalculationData) == 0 {
		t.Fatal("expected calculation_data to be populated")
	}
}

func TestCalculateApartment_StepByStepMath(t *testing.T) {
	repo := newFormulaMockRepo()
	svc := NewAppraisalService(repo, stubStorage{})

	input := domain.ApartmentCalculationInput{
		Subject: domain.ApartmentObjectInput{
			Area:          50.0,
			District:      "Центральный", // 120,000
			Condition:     domain.ConditionGood,
			RepairClassID: "class_2",
			Floor:         domain.FloorMiddle,
		},
		Analogs: []domain.ApartmentAnalogInput{
			{
				PricePerSqM:       100000,
				BargainingPercent: 5.0,
				Area:              50.0,
				District:          "Центральный", // 120,000 => k1 = 1.0
				Condition:         domain.ConditionGood, // Good vs Good => k3 = 1.0
				RepairClassID:     "class_2",
				Floor:             domain.FloorMiddle, // Middle vs Middle => k4 = 1.0
			},
			{
				PricePerSqM:       110000,
				BargainingPercent: 0.0,
				Area:              50.0,
				District:          "Центральный",
				Condition:         domain.ConditionGood,
				RepairClassID:     "class_2",
				Floor:             domain.FloorMiddle,
			},
			{
				PricePerSqM:       120000,
				BargainingPercent: 10.0,
				Area:              50.0,
				District:          "Центральный",
				Condition:         domain.ConditionGood,
				RepairClassID:     "class_2",
				Floor:             domain.FloorMiddle,
			},
			{
				PricePerSqM:       100000,
				BargainingPercent: 0.0,
				Area:              50.0,
				District:          "Центральный",
				Condition:         domain.ConditionGood,
				RepairClassID:     "class_2",
				Floor:             domain.FloorMiddle,
			},
		},
	}

	res, err := svc.CalculateApartment(context.Background(), input)
	if err != nil {
		t.Fatalf("CalculateApartment failed: %v", err)
	}

	// Analog 0: 100,000 - 5% = 95,000. Since all other attributes match subject, final adjusted price is 95,000.
	if res.Analogs[0].PriceAfterBargaining != 95000 {
		t.Errorf("expected 95000 after bargaining, got %f", res.Analogs[0].PriceAfterBargaining)
	}
	if res.Analogs[0].PriceAfterFloor != 95000 {
		t.Errorf("expected 95000 final adjusted price, got %f", res.Analogs[0].PriceAfterFloor)
	}

	// Analog 1: 110,000 - 0% = 110,000.
	if res.Analogs[1].PriceAfterBargaining != 110000 {
		t.Errorf("expected 110000 after bargaining, got %f", res.Analogs[1].PriceAfterBargaining)
	}

	// Analog 2: 120,000 - 10% = 108,000.
	if res.Analogs[2].PriceAfterBargaining != 108000 {
		t.Errorf("expected 108000 after bargaining, got %f", res.Analogs[2].PriceAfterBargaining)
	}

	// Analog 3: 100,000 - 0% = 100,000.
	if res.Analogs[3].PriceAfterBargaining != 100000 {
		t.Errorf("expected 100000 after bargaining, got %f", res.Analogs[3].PriceAfterBargaining)
	}

	if res.WeightedPricePerSqM <= 0 {
		t.Errorf("expected positive weighted price, got %f", res.WeightedPricePerSqM)
	}
	if res.TotalMarketValue <= 0 {
		t.Errorf("expected positive total market value, got %f", res.TotalMarketValue)
	}
}
