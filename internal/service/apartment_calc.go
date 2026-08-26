package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/appraisal-crm/review-service/internal/domain"
	"github.com/appraisal-crm/review-service/internal/repository"
)

var (
	ErrInvalidCalculationInput = errors.New("invalid calculation input: must provide subject and exactly 4 analogs")
)

func (s *appraisalService) GetApartmentFormulaConfig(ctx context.Context) (*domain.ApartmentFormulaConfig, error) {
	cfg, err := s.repo.GetFormulaConfig(ctx, "apartment")
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			defaultCfg := domain.DefaultApartmentFormulaConfig()
			_ = s.repo.SaveFormulaConfig(ctx, &defaultCfg)
			return &defaultCfg, nil
		}
		return nil, err
	}
	return cfg, nil
}

func (s *appraisalService) UpdateApartmentFormulaConfig(ctx context.Context, cfg *domain.ApartmentFormulaConfig, updatedBy string) (*domain.ApartmentFormulaConfig, error) {
	cfg.ID = "apartment"
	cfg.UpdatedAt = time.Now()
	cfg.UpdatedBy = &updatedBy

	if err := s.repo.SaveFormulaConfig(ctx, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (s *appraisalService) ResetApartmentFormulaConfig(ctx context.Context, updatedBy string) (*domain.ApartmentFormulaConfig, error) {
	defaultCfg := domain.DefaultApartmentFormulaConfig()
	defaultCfg.UpdatedAt = time.Now()
	defaultCfg.UpdatedBy = &updatedBy

	if err := s.repo.SaveFormulaConfig(ctx, &defaultCfg); err != nil {
		return nil, err
	}
	return &defaultCfg, nil
}

func (s *appraisalService) CalculateApartment(ctx context.Context, in domain.ApartmentCalculationInput) (*domain.ApartmentCalculationResult, error) {
	if in.Subject.Area <= 0 || len(in.Analogs) != 4 {
		return nil, ErrInvalidCalculationInput
	}
	for _, a := range in.Analogs {
		if a.PricePerSqM <= 0 || a.Area <= 0 {
			return nil, ErrInvalidCalculationInput
		}
	}

	cfg, err := s.GetApartmentFormulaConfig(ctx)
	if err != nil {
		return nil, err
	}

	return calculateApartmentWithConfig(in, cfg)
}

func calculateApartmentWithConfig(in domain.ApartmentCalculationInput, cfg *domain.ApartmentFormulaConfig) (*domain.ApartmentCalculationResult, error) {
	// Lookup maps for fast access
	districtMap := make(map[string]float64)
	for _, dp := range cfg.DistrictPrices {
		districtMap[dp.Name] = dp.Price
	}

	repairMap := make(map[string]float64)
	for _, rc := range cfg.RepairClasses {
		repairMap[rc.ID] = rc.UnitCost
		repairMap[rc.Name] = rc.UnitCost
	}

	subjDistrictPrice := districtMap[in.Subject.District]
	if subjDistrictPrice <= 0 {
		subjDistrictPrice = 100000 // reasonable default fallback
	}

	subjCondCoeff, ok := cfg.ConditionCoefficients[in.Subject.Condition]
	if !ok {
		subjCondCoeff = 0.67
	}

	subjRepairCost := repairMap[in.Subject.RepairClassID]

	// Scale formula for subject: y = a * (s ^ b)
	subjY := cfg.ScaleFormula.A * math.Pow(in.Subject.Area, cfg.ScaleFormula.B)

	analogsResult := make([]domain.ApartmentAnalogResult, 4)
	var sumM float64

	for i, analog := range in.Analogs {
		res := domain.ApartmentAnalogResult{
			Index:             i + 1,
			InitialPrice:      analog.PricePerSqM,
			BargainingPercent: analog.BargainingPercent,
			Area:              analog.Area,
			District:          analog.District,
			Condition:         analog.Condition,
			RepairClassID:     analog.RepairClassID,
			Floor:             analog.Floor,
		}

		// Step 1: Bargaining
		// x1 = x * (1 - k/100)
		res.PriceAfterBargaining = analog.PricePerSqM * (1.0 - (analog.BargainingPercent / 100.0))
		res.BargainingAbsAdj = math.Abs(analog.BargainingPercent)

		// Step 2: Location
		analogDistrictPrice := districtMap[analog.District]
		if analogDistrictPrice <= 0 {
			analogDistrictPrice = 100000
		}

		var k1 float64
		switch cfg.LocationFormulaType {
		case "doc_formula":
			if analogDistrictPrice > 0 {
				k1 = 1.0 + ((subjDistrictPrice - analogDistrictPrice) / analogDistrictPrice)
			} else {
				k1 = 1.0
			}
		case "ratio":
			k1 = subjDistrictPrice / analogDistrictPrice
		default: // "standard"
			if analog.District == in.Subject.District {
				k1 = 1.0
			} else {
				k1 = subjDistrictPrice / analogDistrictPrice
			}
		}
		res.LocationCoeff = roundTo(k1, 4)
		res.PriceAfterLocation = res.PriceAfterBargaining * k1
		res.LocationAbsAdj = math.Abs(k1-1.0) * 100.0

		// Step 3: Area Scale
		analogY := cfg.ScaleFormula.A * math.Pow(analog.Area, cfg.ScaleFormula.B)
		k2 := 1.0
		if analogY > 0 {
			k2 = subjY / analogY
		}
		res.AreaCoeff = roundTo(k2, 4)
		res.PriceAfterArea = res.PriceAfterLocation * k2
		res.AreaAbsAdj = math.Abs(k2-1.0) * 100.0

		// Step 4: Repair Condition & Class
		analogCondCoeff, ok := cfg.ConditionCoefficients[analog.Condition]
		if !ok {
			analogCondCoeff = 0.67
		}
		analogRepairCost := repairMap[analog.RepairClassID]

		repairDelta := (subjRepairCost*subjCondCoeff - analogRepairCost*analogCondCoeff) / in.Subject.Area
		res.RepairCostDelta = roundTo(repairDelta, 2)

		var k3 float64
		switch cfg.RepairFormulaType {
		case "multiplicative_direct":
			if res.PriceAfterArea > 0 {
				k3 = 1.0 + (repairDelta / res.PriceAfterArea)
			} else {
				k3 = 1.0
			}
			res.PriceAfterRepair = res.PriceAfterArea * k3
		default: // "additive_equivalent"
			if res.PriceAfterArea > 0 {
				k3 = 1.0 + (repairDelta / res.PriceAfterArea)
			} else {
				k3 = 1.0
			}
			res.PriceAfterRepair = res.PriceAfterArea + repairDelta
		}
		res.RepairCoeff = roundTo(k3, 4)
		res.RepairAbsAdj = math.Abs(k3-1.0) * 100.0

		// Step 5: Floor
		k4 := 1.0
		if floorMap, ok := cfg.FloorMatrix[in.Subject.Floor]; ok {
			if fVal, ok := floorMap[analog.Floor]; ok {
				k4 = fVal
			}
		}
		res.FloorCoeff = roundTo(k4, 4)
		res.PriceAfterFloor = res.PriceAfterRepair * k4
		res.FloorAbsAdj = math.Abs(k4-1.0) * 100.0

		// Step 6 (part 1): Absolute adjustments sum M_j
		res.TotalAbsAdjustment = roundTo(res.BargainingAbsAdj+res.LocationAbsAdj+res.AreaAbsAdj+res.RepairAbsAdj+res.FloorAbsAdj, 2)
		sumM += res.TotalAbsAdjustment

		analogsResult[i] = res
	}

	// Step 6 (part 2): Weights reconciliation
	// Weight_j = (1 - M_j / sumM) / 3
	var weightedPricePerSqM float64
	for i := range analogsResult {
		var w float64
		if sumM > 0 {
			w = (1.0 - (analogsResult[i].TotalAbsAdjustment / sumM)) / 3.0
		} else {
			w = 0.25
		}
		analogsResult[i].Weight = roundTo(w, 4)
		weightedPricePerSqM += analogsResult[i].PriceAfterFloor * w
	}

	weightedPricePerSqM = roundTo(weightedPricePerSqM, 2)
	totalMarketValue := roundTo(weightedPricePerSqM*in.Subject.Area, 2)

	return &domain.ApartmentCalculationResult{
		Subject:             in.Subject,
		Analogs:             analogsResult,
		SumM:                roundTo(sumM, 2),
		WeightedPricePerSqM: weightedPricePerSqM,
		TotalMarketValue:    totalMarketValue,
		CalculatedAt:        time.Now(),
	}, nil
}

func roundTo(val float64, places int) float64 {
	shift := math.Pow(10, float64(places))
	return math.Round(val*shift) / shift
}

func formatFloatString(val float64) string {
	return fmt.Sprintf("%.2f", val)
}
