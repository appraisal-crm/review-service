-- Formula configurations (admin-editable).
CREATE TABLE formula_configs (
    id         TEXT PRIMARY KEY,
    config     JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by TEXT
);

-- Seed default apartment appraisal formula configuration
INSERT INTO formula_configs (id, config, updated_at, updated_by)
VALUES (
    'apartment',
    '{
        "scale_formula": {
            "a": 1.17,
            "b": -0.05
        },
        "district_prices": [
            {"name": "Центральный", "price": 120000},
            {"name": "Северный", "price": 100000},
            {"name": "Южный", "price": 95000},
            {"name": "Западный", "price": 110000},
            {"name": "Восточный", "price": 90000},
            {"name": "Пригород", "price": 75000}
        ],
        "condition_coefficients": {
            "excellent": 1.00,
            "good": 0.67,
            "satisfactory": 0.33,
            "unsatisfactory": 0.00
        },
        "repair_classes": [
            {"id": "class_1", "name": "Класс 1", "unit_cost": 20.0},
            {"id": "class_2", "name": "Класс 2", "unit_cost": 15.0},
            {"id": "class_3", "name": "Класс 3", "unit_cost": 10.0},
            {"id": "class_4", "name": "Класс 4", "unit_cost": 5.0},
            {"id": "class_5", "name": "Класс 5", "unit_cost": 1.0}
        ],
        "floor_matrix": {
            "first": {
                "first": 1.00,
                "middle": 0.97,
                "last": 0.98
            },
            "middle": {
                "first": 1.03,
                "middle": 1.00,
                "last": 1.01
            },
            "last": {
                "first": 1.02,
                "middle": 0.99,
                "last": 1.00
            }
        },
        "location_formula_type": "doc_formula",
        "repair_formula_type": "multiplicative_direct"
    }'::jsonb,
    NOW(),
    'system'
)
ON CONFLICT (id) DO NOTHING;

-- Store calculation details directly on the appraisal
ALTER TABLE appraisals ADD COLUMN IF NOT EXISTS calculation_data JSONB;
