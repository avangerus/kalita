using Xunit;
using Kalita.Domain.Entities;

public class EstimateTests
{
    [Fact]
    public void Estimate_Creation_Works()
    {
        var estimate = new Estimate
        {
            Id = Guid.NewGuid(),
            Name = "Смета по проекту",
            Amount = 1000,
            Status = "Draft"
        };
        Assert.Equal("Draft", estimate.Status);
    }
}
