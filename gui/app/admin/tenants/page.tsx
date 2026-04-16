import Link from "next/link";
import { redirect } from "next/navigation";
import { revalidatePath } from "next/cache";

import {
  APIError,
  createTenant,
  deleteTenant,
  listTenants,
  Tenant,
} from "@/lib/api";
import { getAuthStatus } from "@/lib/auth";

type PageProps = {
  searchParams: Promise<{ error?: string; created?: string }>;
};

export default async function TenantsAdminPage({ searchParams }: PageProps) {
  const { error, created } = await searchParams;
  const auth = await getAuthStatus();
  if (!auth) redirect("/login?next=/admin/tenants");

  let tenants: Tenant[] | null;
  try {
    tenants = await listTenants();
  } catch (err) {
    tenants = null;
    // APIError here means the server returned a non-auth failure —
    // re-surface the message so operators see root cause, not a blank
    // page.
    if (err instanceof APIError) {
      return (
        <div>
          <h1 className="page-title">Tenants</h1>
          <div className="banner banner-error">{err.message}</div>
        </div>
      );
    }
    throw err;
  }

  if (tenants === null) {
    return (
      <div>
        <h1 className="page-title">Tenants</h1>
        <div className="banner banner-warn">
          Your API key is valid but cannot list tenants. This page requires an
          admin-role key. <Link href="/login">Switch keys</Link>
        </div>
      </div>
    );
  }

  async function createAction(formData: FormData) {
    "use server";
    const name = (formData.get("name") ?? "").toString().trim();
    if (!name) redirect("/admin/tenants?error=name+is+required");
    try {
      const tenant = await createTenant(name);
      revalidatePath("/admin/tenants");
      redirect(`/admin/tenants?created=${encodeURIComponent(tenant.id)}`);
    } catch (err) {
      if (err instanceof APIError) {
        redirect(`/admin/tenants?error=${encodeURIComponent(err.message)}`);
      }
      throw err;
    }
  }

  async function deleteAction(formData: FormData) {
    "use server";
    const id = (formData.get("id") ?? "").toString();
    if (!id) return;
    try {
      await deleteTenant(id);
      revalidatePath("/admin/tenants");
    } catch (err) {
      if (err instanceof APIError) {
        redirect(`/admin/tenants?error=${encodeURIComponent(err.message)}`);
      }
      throw err;
    }
  }

  return (
    <div>
      <div className="breadcrumb">
        <Link href="/">Agents</Link> · Admin
      </div>
      <h1 className="page-title">Tenants</h1>
      <p className="page-lede">
        One row per tenant in <code>.mockagents-tenancy.db</code>. Deleting a
        tenant cascades to its API keys — there is no soft-delete.
      </p>

      {error && <div className="banner banner-error">{error}</div>}
      {created && (
        <div className="banner banner-ok">
          Tenant <code>{created}</code> created. Open it to mint its first API key.
        </div>
      )}

      <form action={createAction} className="inline-form">
        <input name="name" placeholder="new tenant name" required />
        <button type="submit" className="btn btn-primary">
          Create tenant
        </button>
      </form>

      {tenants.length === 0 ? (
        <p className="muted">No tenants yet.</p>
      ) : (
        <table className="data-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Name</th>
              <th>Created</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {tenants.map((t) => (
              <tr key={t.id}>
                <td>
                  <Link href={`/admin/tenants/${encodeURIComponent(t.id)}`}>
                    <code>{t.id}</code>
                  </Link>
                </td>
                <td>{t.name}</td>
                <td className="muted">{t.created_at}</td>
                <td>
                  <form action={deleteAction} className="inline">
                    <input type="hidden" name="id" value={t.id} />
                    <button type="submit" className="btn btn-danger">
                      Delete
                    </button>
                  </form>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
