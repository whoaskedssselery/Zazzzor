// An icon next to text in one JSX element needs something between them: gap
// or space-x on the element, or a margin on the icon. Without it the two
// touch, as «Выйти» once did in the user settings. An icon is a capitalised
// self-closing element with aria-hidden (how this project writes lucide
// icons); text is a JSX text node, an inline text element or an expression.
// Components that space their own children are listed in `spacedComponents`.

const GAP = /\b(gap-|space-x-)/;
const ICON_MARGIN = /(^|[\s"'`])-?m[lrxse]-/;
const INLINE_TEXT = /^(span|b|strong|bdi|em|small)$/;

const nameOf = (node) => {
  if (node.type === "JSXIdentifier") return node.name;
  if (node.type === "JSXMemberExpression") return `${nameOf(node.object)}.${nameOf(node.property)}`;
  return "";
};

/** @type {import("eslint").Rule.RuleModule} */
const rule = {
  meta: {
    type: "problem",
    docs: { description: "Keep a gap between an icon and the text beside it." },
    schema: [{ type: "object", properties: { spacedComponents: { type: "array", items: { type: "string" } } } }],
    messages: { touching: "This icon touches the text beside it: add gap-* to the element or a margin to the icon." },
  },
  create(context) {
    const spaced = new Set(context.options[0]?.spacedComponents ?? []);
    const source = context.sourceCode;
    const classOf = (opening) => {
      const attr = opening.attributes.find((a) => a.type === "JSXAttribute" && a.name.name === "className");
      return attr?.value ? source.getText(attr.value) : "";
    };
    const isIcon = (n) =>
      n.type === "JSXElement" &&
      n.openingElement.selfClosing &&
      /^[A-Z]/.test(nameOf(n.openingElement.name)) &&
      n.openingElement.attributes.some((a) => a.type === "JSXAttribute" && a.name.name === "aria-hidden");
    const isText = (n) =>
      (n.type === "JSXText" && n.value.trim() !== "") ||
      (n.type === "JSXElement" && INLINE_TEXT.test(nameOf(n.openingElement.name))) ||
      (n.type === "JSXExpressionContainer" &&
        n.expression.type !== "JSXEmptyExpression" &&
        n.expression.type !== "JSXElement" &&
        !/^[A-Z]/.test(source.getText(n.expression)));
    return {
      JSXElement(node) {
        if (spaced.has(nameOf(node.openingElement.name)) || GAP.test(classOf(node.openingElement))) return;
        const kids = node.children.filter((c) => !(c.type === "JSXText" && c.value.trim() === ""));
        for (let i = 0; i + 1 < kids.length; i++) {
          const [a, b] = [kids[i], kids[i + 1]];
          const icon = isIcon(a) ? a : isIcon(b) ? b : null;
          if (icon === null || !isText(icon === a ? b : a)) continue;
          if (ICON_MARGIN.test(classOf(icon.openingElement))) continue;
          context.report({ node: icon, messageId: "touching" });
        }
      },
    };
  },
};

export default { rules: { "icon-text-gap": rule } };
