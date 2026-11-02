"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import Link from "next/link";
import { useEffect, useId, useMemo } from "react";
import { useForm, useWatch } from "react-hook-form";
import { z } from "zod";
import { Button, StatusPill } from "@/components/ui";
import { Dialog } from "@/components/ui/dialog";
import type { PaymentMethod } from "@/lib/api/schemas";
import {
  useCreatePayment,
  type PaymentIntent,
} from "@/lib/hooks/useCreatePayment";
import {
  currencies,
  formatMoney,
  minorToMajorInput,
  parseMajorAmount,
} from "@/lib/money";

export const rails = ["mock", "card", "ach"] as const;

const formSchema = z
  .object({
    amount: z.string(),
    currency: z.enum(["USD", "EUR", "GBP", "JPY", "KWD"]),
    rail: z.enum(rails),
    payment_method_id: z.string().min(1, "Choose a sandbox payment method"),
    merchant_reference: z
      .string()
      .max(255, "Use at most 255 characters")
      .refine(
        (value) => new TextEncoder().encode(value).length <= 255,
        "Use at most 255 bytes",
      ),
  })
  .superRefine((values, context) => {
    const parsed = parseMajorAmount(values.amount, values.currency);
    if (!parsed.ok)
      context.addIssue({
        code: "custom",
        path: ["amount"],
        message: parsed.message,
      });
  });
type FormValues = z.infer<typeof formSchema>;

function toIntent(values: FormValues): PaymentIntent {
  const parsed = parseMajorAmount(values.amount, values.currency);
  if (!parsed.ok) throw new Error(parsed.message);
  const intent: PaymentIntent = {
    amount_minor: parsed.amountMinor,
    currency: values.currency,
    payment_method_id: values.payment_method_id,
  };
  const reference = values.merchant_reference.trim();
  if (reference) intent.merchant_reference = reference;
  return intent;
}

const serverFieldMap: Record<string, keyof FormValues> = {
  amount_minor: "amount",
  currency: "currency",
  payment_method_id: "payment_method_id",
  merchant_reference: "merchant_reference",
};

function Field({
  id,
  label,
  error,
  hint,
  children,
}: {
  id: string;
  label: string;
  error?: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      {children}
      {hint && (
        <p id={`${id}-hint`} className="field-hint">
          {hint}
        </p>
      )}
      {error && (
        <p id={`${id}-error`} className="field-error" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}

/**
 * Creates a sandbox payment from an existing payment-method reference. It
 * never collects card numbers or bank credentials. Success is never shown
 * optimistically: the dialog only reports what the API confirmed.
 */
export function CreatePaymentDialog({
  open,
  onClose,
  methods,
  resume,
}: {
  open: boolean;
  onClose: () => void;
  methods: PaymentMethod[];
  /** An unconfirmed intent restored from this tab's session. */
  resume?: PaymentIntent | null;
}) {
  const formId = useId();
  const { submit, outcome, isPending, reset } = useCreatePayment();
  const defaults = useMemo<FormValues>(() => {
    const method = methods.find(
      (candidate) => candidate.id === resume?.payment_method_id,
    );
    // Prefer the seeded "Mock success" method (…0121), then any mock method.
    const firstMock =
      methods.find(
        (candidate) =>
          candidate.rail === "mock" && candidate.id.endsWith("0121"),
      ) ?? methods.find((candidate) => candidate.rail === "mock");
    const currency = (resume?.currency ?? "USD") as FormValues["currency"];
    return {
      amount: resume ? minorToMajorInput(resume.amount_minor, currency) : "",
      currency,
      rail: ((method ?? firstMock ?? methods[0])?.rail ??
        "mock") as FormValues["rail"],
      payment_method_id: (method ?? firstMock ?? methods[0])?.id ?? "",
      merchant_reference: resume?.merchant_reference ?? "",
    };
  }, [methods, resume]);
  const {
    register,
    handleSubmit,
    control,
    setValue,
    setError,
    reset: resetForm,
    formState: { errors },
  } = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: defaults,
  });

  useEffect(() => {
    if (open) resetForm(defaults);
  }, [open, defaults, resetForm]);

  const rail = useWatch({ control, name: "rail" });
  const methodId = useWatch({ control, name: "payment_method_id" });
  const railMethods = methods.filter((method) => method.rail === rail);
  const firstRailMethod = railMethods[0]?.id ?? "";
  const methodMatchesRail = railMethods.some(
    (method) => method.id === methodId,
  );
  useEffect(() => {
    if (!methodMatchesRail && firstRailMethod !== methodId)
      setValue("payment_method_id", firstRailMethod);
  }, [methodMatchesRail, firstRailMethod, methodId, setValue]);

  useEffect(() => {
    if (outcome.kind !== "rejected") return;
    for (const field of outcome.error.details.field_errors) {
      const name = serverFieldMap[field.field];
      if (name) setError(name, { type: "server", message: field.message });
    }
  }, [outcome, setError]);

  const close = () => {
    if (isPending) return; // keep the dialog while the API has not answered
    reset();
    onClose();
  };
  const onSubmit = handleSubmit((values) => submit(toIntent(values)));
  const locked = isPending || outcome.kind === "accepted";

  return (
    <Dialog
      open={open}
      onClose={close}
      title="Create sandbox payment"
      className="create-payment-dialog"
    >
      <p className="sandbox-notice">
        <strong>Sandbox only — no real funds.</strong> Uses an existing sandbox
        payment-method reference; card numbers and bank credentials are never
        collected here.
      </p>
      {outcome.kind === "accepted" ? (
        <section className="submission-result" aria-live="polite">
          <h3>Payment accepted — outcome pending</h3>
          <p>
            The API accepted this request (HTTP 202)
            {outcome.replayed
              ? " and replayed the original response for this idempotency key, so no second payment was created"
              : ""}
            . It is not complete: its final outcome appears on the payment page
            only after the API confirms it.
          </p>
          <dl className="detail-list">
            <div>
              <dt>Payment</dt>
              <dd className="mono">{outcome.result.data.id}</dd>
            </div>
            <div>
              <dt>Amount</dt>
              <dd>
                {formatMoney(
                  outcome.result.data.amount_minor,
                  outcome.result.data.currency,
                )}
              </dd>
            </div>
            <div>
              <dt>Current status</dt>
              <dd>
                <StatusPill status={outcome.result.data.status} />
              </dd>
            </div>
          </dl>
          <div className="dialog-actions">
            <Link
              className="button"
              href={`/dashboard/payments/${outcome.result.data.id}`}
            >
              View payment status
            </Link>
            <Button className="button-quiet" onClick={close}>
              Done
            </Button>
          </div>
        </section>
      ) : (
        <form id={formId} onSubmit={onSubmit} noValidate>
          <div className="form-grid">
            <Field
              id={`${formId}-amount`}
              label="Amount"
              error={errors.amount?.message}
              hint="In major units, for example 250.00"
            >
              <input
                id={`${formId}-amount`}
                inputMode="decimal"
                autoComplete="off"
                placeholder="100.00"
                disabled={locked}
                aria-invalid={Boolean(errors.amount)}
                aria-describedby={`${formId}-amount-hint${errors.amount ? ` ${formId}-amount-error` : ""}`}
                {...register("amount")}
              />
            </Field>
            <Field
              id={`${formId}-currency`}
              label="Currency"
              error={errors.currency?.message}
            >
              <select
                id={`${formId}-currency`}
                disabled={locked}
                {...register("currency")}
              >
                {currencies.map((currency) => (
                  <option key={currency} value={currency}>
                    {currency}
                  </option>
                ))}
              </select>
            </Field>
            <Field
              id={`${formId}-rail`}
              label="Rail"
              error={errors.rail?.message}
            >
              <select
                id={`${formId}-rail`}
                disabled={locked}
                {...register("rail")}
              >
                {rails.map((value) => (
                  <option key={value} value={value}>
                    {value}
                  </option>
                ))}
              </select>
            </Field>
            <Field
              id={`${formId}-method`}
              label="Payment method"
              error={
                errors.payment_method_id?.message ??
                (railMethods.length === 0
                  ? "No sandbox payment method exists for this rail"
                  : undefined)
              }
            >
              <select
                id={`${formId}-method`}
                disabled={locked || railMethods.length === 0}
                aria-invalid={Boolean(errors.payment_method_id)}
                {...register("payment_method_id")}
              >
                {railMethods.map((method) => (
                  <option key={method.id} value={method.id}>
                    {method.brand ?? method.rail} ···· {method.last_four ?? "—"}{" "}
                    ({method.id.slice(-4)})
                  </option>
                ))}
              </select>
            </Field>
            <Field
              id={`${formId}-reference`}
              label="Merchant reference (optional)"
              error={errors.merchant_reference?.message}
            >
              <input
                id={`${formId}-reference`}
                autoComplete="off"
                disabled={locked}
                aria-invalid={Boolean(errors.merchant_reference)}
                {...register("merchant_reference")}
              />
            </Field>
          </div>
          {outcome.kind === "pending" && (
            <p className="submission-status" role="status">
              Submitting… waiting for the API to confirm acceptance.
            </p>
          )}
          {outcome.kind === "unconfirmed" && (
            <section className="submission-warning" role="alert">
              <h3>Outcome unknown — checking status</h3>
              <p>
                The API did not confirm this submission ({outcome.error.message}
                ). The payment may or may not exist. Submitting this unchanged
                request again resends the same Idempotency-Key, so the API
                returns the original payment instead of creating a second one.
                Editing any field starts a new payment intent with a new key.
              </p>
              <p className="muted">
                Request ID:{" "}
                <code className="mono">{outcome.error.requestId}</code>
              </p>
            </section>
          )}
          {outcome.kind === "rejected" && (
            <section className="submission-error" role="alert">
              <h3>Payment not created</h3>
              <p>{outcome.error.message}</p>
              <p className="muted">
                Request ID:{" "}
                <code className="mono">{outcome.error.requestId}</code>
              </p>
            </section>
          )}
          <div className="dialog-actions">
            <Button
              type="submit"
              disabled={isPending}
              aria-disabled={isPending}
            >
              {isPending
                ? "Submitting…"
                : outcome.kind === "unconfirmed"
                  ? "Resend unchanged request"
                  : "Create sandbox payment"}
            </Button>
            <Button
              type="button"
              className="button-quiet"
              onClick={close}
              disabled={isPending}
            >
              Cancel
            </Button>
          </div>
        </form>
      )}
    </Dialog>
  );
}
