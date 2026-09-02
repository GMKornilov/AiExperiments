export type ChatMode = "free" | "controlled";

export type Recipe = {
  coffee_g: number;
  water_g: number;
  temperature_c: number;
  brew_time_sec: number;
};

export type ControlledAnswer = {
  summary: string;
  focus_points: string[];
  recipe: Recipe;
};

export type ChatResponse = {
  mode: ChatMode;
  raw: string;
  data: unknown;
};

export type ErrorResponse = {
  error: string;
};

export function isControlledAnswer(value: unknown): value is ControlledAnswer {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return false;
  }

  const candidate = value as Partial<ControlledAnswer>;
  return (
    typeof candidate.summary === "string" &&
    Array.isArray(candidate.focus_points) &&
    candidate.focus_points.every((point) => typeof point === "string") &&
    Boolean(candidate.recipe) &&
    typeof candidate.recipe === "object"
  );
}
