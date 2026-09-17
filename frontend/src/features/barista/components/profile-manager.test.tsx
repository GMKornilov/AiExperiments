import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { baristaClient } from "../lib/chat-client";
import { ProfileManager } from "./profile-manager";

vi.mock("../lib/chat-client", async () => {
  const actual = await vi.importActual<typeof import("../lib/chat-client")>("../lib/chat-client");
  return { ...actual, baristaClient: { createProfile: vi.fn(), selectProfile: vi.fn(), removeProfile: vi.fn(), profiles: vi.fn() } };
});

const listing = { profiles: [
  { id: "barista", name: "Бариста", style: "Дружелюбно", constraints: "Без выдумок", additional_context: "Рецепты", built_in: true },
  { id: "home", name: "Дом", style: "Кратко", constraints: "Без молока", additional_context: "V60", built_in: false },
], active_profile_id: "barista" };

describe("ProfileManager", () => {
  afterEach(() => { cleanup(); vi.clearAllMocks(); });
  it("не создаёт профиль, пока не заполнены все свободные поля", () => {
    render(<ProfileManager initial={listing} loading={false} error={null} onChange={vi.fn()} onClose={vi.fn()} onRetry={vi.fn()} />);
    fireEvent.change(screen.getByLabelText("Название"), { target: { value: "Дом" } });
    fireEvent.click(screen.getByRole("button", { name: "Создать и выбрать" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Опишите стиль ответа.");
    expect(baristaClient.createProfile).not.toHaveBeenCalled();
  });

  it("требует подтверждения перед удалением custom профиля", async () => {
    vi.mocked(baristaClient.removeProfile).mockResolvedValue(undefined);
    vi.mocked(baristaClient.profiles).mockResolvedValue(listing);
    render(<ProfileManager initial={listing} loading={false} error={null} onChange={vi.fn()} onClose={vi.fn()} onRetry={vi.fn()} />);
    fireEvent.click(within(screen.getByRole("region", { name: "Доступные профили" })).getByRole("button", { name: "Удалить" }));
    expect(screen.getByRole("alertdialog")).toHaveTextContent("Удалить профиль «Дом»?");
    expect(baristaClient.removeProfile).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Отмена" }));
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    fireEvent.click(within(screen.getByRole("region", { name: "Доступные профили" })).getByRole("button", { name: "Удалить" }));
    fireEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "Удалить" }));
    await waitFor(() => expect(baristaClient.removeProfile).toHaveBeenCalledWith("home"));
  });
});
