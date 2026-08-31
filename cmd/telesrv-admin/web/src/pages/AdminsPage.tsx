import { KeyRound, RefreshCw, ShieldPlus, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState, type FormEvent } from "react";
import { api, errorMessage } from "../api";
import { Alert, EmptyRow, PageFrame } from "../components/ui";
import { useI18n } from "../i18n";
import type { AdminAccountSession, AdminSession, AdminUserRow } from "../types";

// Panel administrator management: the account roster, creation of additional
// administrators, password rotation (self-service included) and the live
// server-side session table with per-session revocation.
export function AdminsPage() {
  const { t } = useI18n();
  const [me, setMe] = useState<AdminSession | null>(null);
  const [admins, setAdmins] = useState<AdminUserRow[]>([]);
  const [sessions, setSessions] = useState<AdminAccountSession[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const [createUsername, setCreateUsername] = useState("");
  const [createPassword, setCreatePassword] = useState("");
  const [createPermissions, setCreatePermissions] = useState("*");
  const [creating, setCreating] = useState(false);

  const [passwordTarget, setPasswordTarget] = useState<string>("");
  const [passwordValue, setPasswordValue] = useState("");

  const canManage = true; // the route itself is permission-gated

  const load = useCallback(async () => {
    setBusy(true);
    setError("");
    try {
      const [sessionResult, adminsResult, sessionsResult] = await Promise.all([
        api.session(),
        api.admins(),
        api.adminSessions()
      ]);
      setMe(sessionResult);
      setAdmins(adminsResult.admins ?? []);
      setSessions(sessionsResult.sessions ?? []);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function run(action: () => Promise<void>, done?: string) {
    setError("");
    setNotice("");
    try {
      await action();
      if (done) setNotice(done);
      await load();
    } catch (err) {
      setError(errorMessage(err));
    }
  }

  async function createAdmin(event: FormEvent) {
    event.preventDefault();
    if (!createUsername.trim() || !createPassword) return;
    setCreating(true);
    await run(async () => {
      await api.createAdmin({
        username: createUsername.trim(),
        password: createPassword,
        permissions: createPermissions.split(",").map((item) => item.trim()).filter(Boolean)
      });
      setCreateUsername("");
      setCreatePassword("");
      setCreatePermissions("*");
    }, undefined);
    setCreating(false);
  }

  async function changePassword(event: FormEvent) {
    event.preventDefault();
    const target = passwordTarget || String(me?.admin_id ?? "");
    if (!target || !passwordValue) return;
    setBusy(true);
    await run(async () => {
      await api.setAdminPassword(Number(target), passwordValue);
      setPasswordValue("");
    });
    setBusy(false);
  }

  async function toggleActive(admin: AdminUserRow) {
    await run(() => api.setAdminActive(admin.id, !admin.active).then(() => undefined));
  }

  function formatDateTime(value: string): string {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
  }

  return (
    <PageFrame
      title={t("route.admins")}
      eyebrow={t("route.adminsSubtitle")}
      actions={
        <button className="btn icon-text" type="button" onClick={() => void load()} disabled={busy}>
          <RefreshCw size={15} className={busy ? "spin" : ""} /> {t("common.refresh")}
        </button>
      }
    >
      {error && <Alert>{error}</Alert>}
      {notice && <Alert>{notice}</Alert>}

      <section className="section-block">
        <div className="dock-title">{t("admins.accountsHeading")}</div>
        <div className="table-wrap">
          <table className="data-table">
            <thead>
              <tr>
                <th>ID</th>
                <th>{t("admins.username")}</th>
                <th>{t("admins.permissionsHint").split(",")[0]}</th>
                <th>{t("admins.active")}</th>
                <th>{t("admins.created")}</th>
                <th>{t("admins.sessionsHeading")}</th>
                {canManage && <th>{t("admins.actions")}</th>}
              </tr>
            </thead>
            <tbody>
              {admins.map((admin) => (
                <tr key={admin.id}>
                  <td className="mono">{admin.id}</td>
                  <td className="mono">
                    {admin.username}
                    {me?.admin_id === admin.id && <span className="login-chip">{t("admins.you")}</span>}
                  </td>
                  <td className="mono">{admin.permissions.join(", ") || "—"}</td>
                  <td className={admin.active ? "mono" : "mono warn"}>{admin.active ? t("admins.active") : t("admins.disabled")}</td>
                  <td className="mono">{formatDateTime(admin.created_at)}</td>
                  <td className="mono">{admin.active_sessions}</td>
                  {canManage && (
                    <td>
                      <button className="btn icon-text" type="button" onClick={() => void toggleActive(admin)}>
                        {admin.active ? t("admins.disableAction") : t("admins.enableAction")}
                      </button>
                    </td>
                  )}
                </tr>
              ))}
              {admins.length === 0 && <EmptyRow colSpan={canManage ? 7 : 6} />}
            </tbody>
          </table>
        </div>
      </section>

      <section className="section-block">
        <div className="dock-title"><ShieldPlus size={15} /> {t("admins.create")}</div>
        <form className="form-stack" onSubmit={(event) => void createAdmin(event)}>
          <label>
            <span>{t("admins.username")}</span>
            <input type="text" value={createUsername} onChange={(event) => setCreateUsername(event.target.value)} />
          </label>
          <label>
            <span>{t("admins.password")}</span>
            <input type="password" value={createPassword} autoComplete="new-password"
              onChange={(event) => setCreatePassword(event.target.value)} />
          </label>
          <label>
            <span>{t("admins.permissionsHint")}</span>
            <input type="text" value={createPermissions} onChange={(event) => setCreatePermissions(event.target.value)} />
          </label>
          <button className="btn primary" type="submit" disabled={creating}>
            {creating ? t("admins.creating") : t("admins.create")}
          </button>
        </form>
      </section>

      <section className="section-block">
        <div className="dock-title"><KeyRound size={15} /> {t("admins.changePassword")}</div>
        <form className="form-stack" onSubmit={(event) => void changePassword(event)}>
          <label>
            <span>{t("admins.username")}</span>
            <select value={passwordTarget || String(me?.admin_id ?? "")} onChange={(event) => setPasswordTarget(event.target.value)}>
              {(me?.admin_id ? [{ id: me.admin_id, username: `${me.username ?? me.actor} (${t("admins.you")})` }] : [])
                .concat(admins.filter((admin) => admin.id !== me?.admin_id))
                .map((admin) => (
                  <option key={admin.id} value={String(admin.id)}>{admin.username}</option>
                ))}
            </select>
          </label>
          <label>
            <span>{t("admins.newPassword")}</span>
            <input type="password" value={passwordValue} autoComplete="new-password"
              onChange={(event) => setPasswordValue(event.target.value)} />
          </label>
          <button className="btn primary" type="submit" disabled={busy}>{t("admins.save")}</button>
        </form>
      </section>

      <section className="section-block">
        <div className="dock-title">{t("admins.sessionsHeading")}</div>
        <div className="table-wrap">
          <table className="data-table">
            <thead>
              <tr>
                <th>{t("admins.username")}</th>
                <th>{t("admins.ipAddress")}</th>
                <th>{t("admins.client")}</th>
                <th>{t("admins.lastSeen")}</th>
                <th>{t("admins.expires")}</th>
                <th>{t("admins.actions")}</th>
              </tr>
            </thead>
            <tbody>
              {sessions.map((session) => (
                <tr key={session.id}>
                  <td className="mono">{session.username}</td>
                  <td className="mono">{session.ip_addr || "—"}</td>
                  <td className="mono truncate" title={session.user_agent}>{session.user_agent.slice(0, 60) || "—"}</td>
                  <td className="mono">{formatDateTime(session.last_seen_at)}</td>
                  <td className="mono">{formatDateTime(session.expires_at)}</td>
                  <td>
                    <button className="btn icon-text" type="button"
                      onClick={() => void run(() => api.revokeAdminSession(session.id).then(() => undefined))}>
                      <Trash2 size={14} /> {t("admins.revokeSession")}
                    </button>
                  </td>
                </tr>
              ))}
              {sessions.length === 0 && <EmptyRow colSpan={6} />}
            </tbody>
          </table>
        </div>
      </section>
    </PageFrame>
  );
}
