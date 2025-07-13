using Microsoft.AspNetCore.Mvc;
using Kalita.Domain.Entities;
using Kalita.Application.Services;
using Kalita.Application.Models;
using Kalita.WebApi.DTO;

namespace Kalita.WebApi.Controllers;

[ApiController]
[Route("api/estimates/{estimateId}/lines")]
public class EstimateLinesController : KalitaBaseController
{
    private readonly EstimateService _service;

    public EstimateLinesController(EstimateService service)
    {
        _service = service;
    }

    [HttpPost]
    public IActionResult Create(Guid estimateId, [FromBody] CreateEstimateLineRequest request)
    {

        var line = _service.AddLine(
            estimateId,
            request.Name,
            request.Amount,
            request.Type
        );
        return Ok(line);
    }

    [HttpGet]
    public IActionResult GetByEstimate(Guid estimateId)
    {
        var lines = _service.GetLines(estimateId);
        return Ok(lines);
    }
}

