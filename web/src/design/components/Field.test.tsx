import { render, screen } from "@testing-library/react";
import { Field, Input } from "./Input";
import { Select } from "./Select";
import { Textarea } from "./Textarea";

const options = [
  { value: "all", label: "Все сообщения" },
  { value: "none", label: "Ничего" },
];

/** The ids in an element's aria-describedby, resolved to their text. */
function description(el: HTMLElement): string {
  return (el.getAttribute("aria-describedby") ?? "")
    .split(" ")
    .filter(Boolean)
    .map((id) => document.getElementById(id)?.textContent ?? `<missing #${id}>`)
    .join(" | ");
}

describe("Field", () => {
  it("labels an Input and links its error through aria-describedby", () => {
    render(
      <Field htmlFor="login" label="Логин" hint="Латиница и цифры." error="Этот логин уже занят.">
        <Input />
      </Field>,
    );
    const input = screen.getByRole("textbox", { name: "Логин" });
    expect(input.id).toBe("login");
    expect(input.getAttribute("aria-invalid")).toBe("true");
    expect(description(input)).toBe("Этот логин уже занят.");
  });

  it("links the hint while there is no error", () => {
    render(
      <Field htmlFor="topic" label="Тема канала" hint="До 1024 символов.">
        <Textarea />
      </Field>,
    );
    const textarea = screen.getByRole("textbox", { name: "Тема канала" });
    expect(textarea.hasAttribute("aria-invalid")).toBe(false);
    expect(description(textarea)).toBe("До 1024 символов.");
  });

  it("labels a Select and links its error through aria-describedby", () => {
    render(
      <Field htmlFor="notify" label="Уведомления" error="Выберите вариант.">
        <Select options={options} required />
      </Field>,
    );
    const trigger = screen.getByRole("combobox", { name: "Уведомления" });
    expect(trigger.id).toBe("notify");
    expect(trigger.getAttribute("aria-invalid")).toBe("true");
    expect(description(trigger)).toBe("Выберите вариант.");
    expect(trigger.getAttribute("aria-required")).toBe("true");
  });

  it("keeps a control's own aria-describedby next to the field's", () => {
    render(
      <>
        <p id="extra">Видно всем.</p>
        <Field htmlFor="name" label="Имя" hint="От 1 до 32 символов.">
          <Input aria-describedby="extra" />
        </Field>
      </>,
    );
    expect(description(screen.getByRole("textbox", { name: "Имя" }))).toBe("От 1 до 32 символов. | Видно всем.");
  });

  it("forwards a ref to the Select trigger", () => {
    let node: HTMLButtonElement | null = null;
    render(
      <Select
        aria-label="Тема"
        options={options}
        ref={(el) => {
          node = el;
        }}
      />,
    );
    expect(node).toBe(screen.getByRole("combobox", { name: "Тема" }));
  });
});
