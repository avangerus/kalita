namespace Kalita.Domain.Entities;
public class User
{
    public Guid Id { get; set; }
    public string FullName { get; set; }
    public string Email { get; set; }
    public List<Role> Roles { get; set; } = new();
}