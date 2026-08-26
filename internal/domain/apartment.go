package domain

import (
	"time"
)

type ApartmentCondition string

const (
	ConditionExcellent       ApartmentCondition = "excellent"
	ConditionGood            ApartmentCondition = "good"
	ConditionSatisfactory    ApartmentCondition = "satisfactory"
	ConditionUnsatisfactory  ApartmentCondition = "unsatisfactory"
)

type ApartmentFloor string

const (
	FloorFirst  ApartmentFloor = "first"
	FloorMiddle ApartmentFloor = "middle"
	FloorLast   ApartmentFloor = "last"
)

type DistrictPrice struct {
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

type RepairClass struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	UnitCost float64 `json:"unit_cost"`
}

type ScaleFormulaConfig struct {
	A float64 `json:"a"` // e.g. 1.17
	B float64 `json:"b"` // e.g. -0.05
}

// FloorMatrix is [FloorOO][FloorOA] -> coefficient
type FloorMatrix map[ApartmentFloor]map[ApartmentFloor]float64

type ApartmentFormulaConfig struct {
	ID                    string                         `json:"id"`
	ScaleFormula          ScaleFormulaConfig             `json:"scale_formula"`
	DistrictPrices        []DistrictPrice                `json:"district_prices"`
	ConditionCoefficients map[ApartmentCondition]float64 `json:"condition_coefficients"`
	RepairClasses         []RepairClass                  `json:"repair_classes"`
	FloorMatrix           FloorMatrix                    `json:"floor_matrix"`
	LocationFormulaType   string                         `json:"location_formula_type"` // "standard", "ratio", "delta_percent"
	RepairFormulaType     string                         `json:"repair_formula_type"`   // "additive_equivalent", "multiplicative_direct"
	UpdatedAt             time.Time                      `json:"updated_at"`
	UpdatedBy             *string                        `json:"updated_by,omitempty"`
}

func DefaultApartmentFormulaConfig() ApartmentFormulaConfig {
	return ApartmentFormulaConfig{
		ID: "apartment",
		ScaleFormula: ScaleFormulaConfig{
			A: 1.17,
			B: -0.05,
		},
		DistrictPrices: []DistrictPrice{
			{Name: "Центральный", Price: 120000},
			{Name: "Северный", Price: 100000},
			{Name: "Южный", Price: 95000},
			{Name: "Западный", Price: 110000},
			{Name: "Восточный", Price: 90000},
			{Name: "Пригород", Price: 75000},
		},
		ConditionCoefficients: map[ApartmentCondition]float64{
			ConditionExcellent:      1.00,
			ConditionGood:           0.67,
			ConditionSatisfactory:   0.33,
			ConditionUnsatisfactory: 0.00,
		},
		RepairClasses: []RepairClass{
			{ID: "class_1", Name: "Класс 1", UnitCost: 20.0},
			{ID: "class_2", Name: "Класс 2", UnitCost: 15.0},
			{ID: "class_3", Name: "Класс 3", UnitCost: 10.0},
			{ID: "class_4", Name: "Класс 4", UnitCost: 5.0},
			{ID: "class_5", Name: "Класс 5", UnitCost: 1.0},
		},
		FloorMatrix: FloorMatrix{
			FloorFirst: {
				FloorFirst:  1.00,
				FloorMiddle: 0.97,
				FloorLast:   0.98,
			},
			FloorMiddle: {
				FloorFirst:  1.03,
				FloorMiddle: 1.00,
				FloorLast:   1.01,
			},
			FloorLast: {
				FloorFirst:  1.02,
				FloorMiddle: 0.99,
				FloorLast:   1.00,
			},
		},
		LocationFormulaType: "doc_formula",
		RepairFormulaType:   "multiplicative_direct",
		UpdatedAt:           time.Now(),
	}
}

// Inputs for apartment calculation
type ApartmentObjectInput struct {
	Area          float64            `json:"area"`
	District      string             `json:"district"`
	Condition     ApartmentCondition `json:"condition"`
	RepairClassID string             `json:"repair_class_id"`
	Floor         ApartmentFloor     `json:"floor"`
}

type ApartmentAnalogInput struct {
	PricePerSqM       float64            `json:"price_per_sqm"`
	BargainingPercent float64            `json:"bargaining_percent"`
	Area              float64            `json:"area"`
	District          string             `json:"district"`
	Condition         ApartmentCondition `json:"condition"`
	RepairClassID     string             `json:"repair_class_id"`
	Floor             ApartmentFloor     `json:"floor"`
}

type ApartmentCalculationInput struct {
	Subject ApartmentObjectInput   `json:"subject"`
	Analogs []ApartmentAnalogInput `json:"analogs"`
}

type ApartmentAnalogResult struct {
	Index                int                `json:"index"`
	InitialPrice         float64            `json:"initial_price"`
	BargainingPercent    float64            `json:"bargaining_percent"`
	Area                 float64            `json:"area"`
	District             string             `json:"district"`
	Condition            ApartmentCondition `json:"condition"`
	RepairClassID        string             `json:"repair_class_id"`
	Floor                ApartmentFloor     `json:"floor"`
	PriceAfterBargaining float64            `json:"price_after_bargaining"` // x1
	LocationCoeff        float64            `json:"location_coeff"`         // k1
	PriceAfterLocation   float64            `json:"price_after_location"`   // x2
	AreaCoeff            float64            `json:"area_coeff"`             // k2
	PriceAfterArea       float64            `json:"price_after_area"`       // x3
	RepairCostDelta      float64            `json:"repair_cost_delta"`      // Стоимость ремонта
	RepairCoeff          float64            `json:"repair_coeff"`           // k3
	PriceAfterRepair     float64            `json:"price_after_repair"`     // x4
	FloorCoeff           float64            `json:"floor_coeff"`            // k4
	PriceAfterFloor      float64            `json:"price_after_floor"`      // x5
	BargainingAbsAdj     float64            `json:"bargaining_abs_adj"`     // |k|
	LocationAbsAdj       float64            `json:"location_abs_adj"`       // |k1|
	AreaAbsAdj           float64            `json:"area_abs_adj"`           // |k2|
	RepairAbsAdj         float64            `json:"repair_abs_adj"`         // |k3|
	FloorAbsAdj          float64            `json:"floor_abs_adj"`          // |k4|
	TotalAbsAdjustment   float64            `json:"total_abs_adjustment"`   // M_j
	Weight               float64            `json:"weight"`                 // Weight_j
}

type ApartmentCalculationResult struct {
	Subject             ApartmentObjectInput    `json:"subject"`
	Analogs             []ApartmentAnalogResult `json:"analogs"`
	SumM                float64                 `json:"sum_m"`
	WeightedPricePerSqM float64                 `json:"weighted_price_per_sqm"`
	TotalMarketValue    float64                 `json:"total_market_value"`
	CalculatedAt        time.Time               `json:"calculated_at"`
}
