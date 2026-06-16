"use client";

import { useState } from "react";
import { toast } from "sonner";
import { createAlertAction } from "./actions";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
type AlertScope = "subject" | "customer" | "global";
type WindowSize = "MINUTE" | "HOUR" | "DAY" | "MONTH";

interface Props {
  meters: { id: string; slug: string }[];
}

export function CreateAlertDialog({ meters }: Props) {
  const [open, setOpen] = useState(false);
  const [meterId, setMeterId] = useState("");
  const [scope, setScope] = useState<AlertScope | "">("");
  const [window, setWindow] = useState<WindowSize | "">("");
  const [threshold, setThreshold] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  function reset() {
    setMeterId("");
    setScope("");
    setWindow("");
    setThreshold("");
    setError(null);
    setLoading(false);
  }

  function handleOpenChange(next: boolean) {
    setOpen(next);
    if (!next) reset();
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!meterId || !scope || !window) {
      setError("Meter, scope, and window are required.");
      return;
    }
    const t = parseFloat(threshold);
    if (!threshold || isNaN(t) || t <= 0) {
      setError("Threshold must be a positive number.");
      return;
    }
    setLoading(true);
    setError(null);
    try {
      await createAlertAction({ meterId, scope: scope as AlertScope, window: window as WindowSize, threshold: t });
      toast.success("Alert rule created");
      handleOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create alert rule. Try again.");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger asChild>
        <Button size="sm">Add alert rule</Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Add alert rule</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4 py-2">
          <div className="space-y-1.5">
            <Label>Meter</Label>
            <Select
              value={meterId}
              onValueChange={setMeterId}
              disabled={loading}
            >
              <SelectTrigger>
                <SelectValue placeholder="Select a meter" />
              </SelectTrigger>
              <SelectContent>
                {meters.map((m) => (
                  <SelectItem key={m.id} value={m.id}>
                    <span className="font-mono">{m.slug}</span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-1.5">
              <Label>Scope</Label>
              <Select
                value={scope}
                onValueChange={(v) => setScope(v as AlertScope)}
                disabled={loading}
              >
                <SelectTrigger>
                  <SelectValue placeholder="Select scope" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="subject">Subject</SelectItem>
                  <SelectItem value="customer">Customer</SelectItem>
                  <SelectItem value="global">Global</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-1.5">
              <Label>Window</Label>
              <Select
                value={window}
                onValueChange={(v) => setWindow(v as WindowSize)}
                disabled={loading}
              >
                <SelectTrigger>
                  <SelectValue placeholder="Select window" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="MINUTE">Minute</SelectItem>
                  <SelectItem value="HOUR">Hour</SelectItem>
                  <SelectItem value="DAY">Day</SelectItem>
                  <SelectItem value="MONTH">Month</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="threshold">Threshold</Label>
            <Input
              id="threshold"
              type="number"
              min="0"
              step="any"
              placeholder="1000000"
              value={threshold}
              onChange={(e) => setThreshold(e.target.value)}
              disabled={loading}
            />
            <p className="text-muted-foreground text-xs">
              Fire when the windowed aggregate exceeds this value.
            </p>
          </div>

          {error && <p className="text-destructive text-sm">{error}</p>}

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => handleOpenChange(false)}
              disabled={loading}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={loading}>
              {loading ? "Adding…" : "Add alert rule"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
